package tonight

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UsageStore is the persistence the spend limits need. ReserveUsage must be
// an atomic check-and-increment: it adds one to the counter under key unless
// that would take it past limit, whatever else is going on at the same time.
type UsageStore interface {
	ReserveUsage(ctx context.Context, key string, limit int, expiresAt time.Time) (count int, ok bool, err error)
	ReleaseUsage(ctx context.Context, key string) error
	UsageCounts(ctx context.Context, keys []string) (map[string]int, error)
}

// Limits are how many model calls may be made per UTC day.
type Limits struct {
	PerUser int // by one user
	Total   int // by everyone together
}

// Default limits. At the default model's price the total caps a day's spend
// at well under a dollar; see docs/API.md for the arithmetic.
const (
	DefaultPerUser = 10
	DefaultTotal   = 200
)

// Which limit refused a call.
const (
	ScopeUser  = "user"
	ScopeTotal = "total"
)

// LimitError says a daily limit is reached. No model call was made.
type LimitError struct {
	Scope    string    // ScopeUser or ScopeTotal
	ResetsAt time.Time // when the next day's budget starts
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("tonight: daily %s limit reached; resets at %s", e.Scope, e.ResetsAt.Format(time.RFC3339))
}

// quota enforces Limits. A model call is paid for whether or not its answer
// turns out to be usable, so a call is counted before it is made and stays
// counted whatever happens to it: an error, a timeout, a refusal or an
// answer that fails validation all cost a place. The limits therefore bound
// what is spent, not what succeeds.
type quota struct {
	store  UsageStore
	limits Limits
	now    func() time.Time
}

// day is one UTC day's pair of counters.
type day struct {
	userKey, totalKey string
	resetsAt          time.Time // the start of the next day
	expiresAt         time.Time // when the counters may be deleted
}

func (q *quota) today(userID primitive.ObjectID) day {
	now := q.now().UTC()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	date := start.Format("2006-01-02")
	return day{
		userKey:  "tonight:" + date + ":user:" + userID.Hex(),
		totalKey: "tonight:" + date + ":total",
		resetsAt: start.AddDate(0, 0, 1),
		// Kept for a day after they stop counting, so yesterday's usage can
		// still be looked at.
		expiresAt: start.AddDate(0, 0, 2),
	}
}

// reserve takes one place in the user's budget and one in everyone's, and
// returns how many calls the user has left today. It returns a *LimitError,
// and takes nothing, when either budget is used up.
//
// The user's budget is taken first. Taken the other way round, a user who is
// over their own limit would briefly hold a place in the shared budget on
// every attempt, and could keep it from others by retrying in a loop.
func (q *quota) reserve(ctx context.Context, userID primitive.ObjectID) (remaining int, err error) {
	d := q.today(userID)

	used, ok, err := q.store.ReserveUsage(ctx, d.userKey, q.limits.PerUser, d.expiresAt)
	if err != nil {
		return 0, fmt.Errorf("reserve the user's budget: %w", err)
	}
	if !ok {
		return 0, &LimitError{Scope: ScopeUser, ResetsAt: d.resetsAt}
	}

	usedTotal, ok, err := q.store.ReserveUsage(ctx, d.totalKey, q.limits.Total, d.expiresAt)
	if err != nil || !ok {
		// Hand the user's place back: no call will be made. If that fails the
		// user is one call short today, which is the safe direction.
		if releaseErr := q.release(d.userKey); releaseErr != nil {
			log.Printf("tonight: could not release a reservation: %v", releaseErr)
		}
		if err != nil {
			return 0, fmt.Errorf("reserve the shared budget: %w", err)
		}
		return 0, &LimitError{Scope: ScopeTotal, ResetsAt: d.resetsAt}
	}
	return q.left(used, usedTotal), nil
}

// release gives a place back. It runs on its own short deadline: the request
// that is giving up may already be out of time.
func (q *quota) release(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return q.store.ReleaseUsage(ctx, key)
}

// remaining returns how many calls the user has left today without taking
// one.
func (q *quota) remaining(ctx context.Context, userID primitive.ObjectID) (int, error) {
	d := q.today(userID)
	counts, err := q.store.UsageCounts(ctx, []string{d.userKey, d.totalKey})
	if err != nil {
		return 0, fmt.Errorf("read the usage counters: %w", err)
	}
	return q.left(counts[d.userKey], counts[d.totalKey]), nil
}

// left is what the user can still use: their own budget, or what is left of
// the shared one when that is less.
func (q *quota) left(used, usedTotal int) int {
	return max(0, min(q.limits.PerUser-used, q.limits.Total-usedTotal))
}
