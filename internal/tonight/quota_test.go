package tonight

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

var midnight = time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)

func wantLimit(t *testing.T, err error, scope string) {
	t.Helper()
	var limitErr *LimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want a *LimitError", err)
	}
	if limitErr.Scope != scope || !limitErr.ResetsAt.Equal(midnight) {
		t.Errorf("limit error = %+v, want scope %q resetting at %s", limitErr, scope, midnight)
	}
}

func TestPerUserLimit(t *testing.T) {
	f := newFixture(t, Limits{PerUser: 2, Total: 200})
	f.addMany("Title", 5)
	other := f.addUser("other@example.com")
	for _, name := range []string{"A", "B", "C", "D"} {
		f.add(title{name: name, user: other})
	}

	for want := 1; want >= 0; want-- {
		if result := f.mustTonight(Request{}); result.RemainingToday != want {
			t.Errorf("remaining = %d, want %d", result.RemainingToday, want)
		}
	}
	for range 3 {
		_, err := f.tonight(Request{})
		wantLimit(t, err, ScopeUser)
	}
	if f.model.called() != 2 {
		t.Errorf("the model was called %d times with a limit of 2", f.model.called())
	}
	// Refused requests take nothing: not from the user, not from the total.
	if user, total := f.store.Usage(userKeyPrefix+f.user.ID.Hex()), f.store.Usage(totalKey); user != 2 || total != 2 {
		t.Errorf("counts = %d for the user and %d in total, want 2 and 2", user, total)
	}

	// One user's limit is not another's.
	if _, err := f.rec.Tonight(context.Background(), other, Request{}); err != nil {
		t.Errorf("another user was refused: %v", err)
	}
}

func TestTotalLimit(t *testing.T) {
	f := newFixture(t, Limits{PerUser: 10, Total: 3})
	f.addMany("Title", 5)
	other := f.addUser("other@example.com")
	for _, name := range []string{"A", "B", "C", "D"} {
		f.add(title{name: name, user: other})
	}

	// The shared budget is smaller than the user's own: it is what is left.
	for want := 2; want >= 0; want-- {
		if result := f.mustTonight(Request{}); result.RemainingToday != want {
			t.Errorf("remaining = %d, want %d", result.RemainingToday, want)
		}
	}
	_, err := f.rec.Tonight(context.Background(), other, Request{})
	wantLimit(t, err, ScopeTotal)
	if f.model.called() != 3 {
		t.Errorf("the model was called %d times with a total limit of 3", f.model.called())
	}
	// The place taken in the other user's own budget was handed back.
	if used := f.store.Usage(userKeyPrefix + other.ID.Hex()); used != 0 {
		t.Errorf("the refused user's count is %d, want 0", used)
	}
	if used := f.store.Usage(totalKey); used != 3 {
		t.Errorf("total count = %d, want 3", used)
	}
}

func TestLimitsResetWithTheUTCDay(t *testing.T) {
	f := newFixture(t, Limits{PerUser: 1, Total: 200})
	f.addMany("Title", 5)

	// 23:59:59 UTC.
	f.rec.quota.now = func() time.Time { return midnight.Add(-time.Second) }
	f.mustTonight(Request{})
	_, err := f.tonight(Request{})
	wantLimit(t, err, ScopeUser)

	// Midnight: a new day, new counters. The time zone of the clock does not
	// matter, only the UTC date.
	f.rec.quota.now = func() time.Time { return midnight.In(time.FixedZone("UTC-8", -8*3600)) }
	if result := f.mustTonight(Request{}); result.RemainingToday != 0 {
		t.Errorf("remaining = %d on the new day's first call, want 0 of 1", result.RemainingToday)
	}
	_, err = f.tonight(Request{})
	var limitErr *LimitError
	if !errors.As(err, &limitErr) || !limitErr.ResetsAt.Equal(midnight.AddDate(0, 0, 1)) {
		t.Errorf("error = %v, want the limit, resetting a day later", err)
	}

	want := []string{
		"tonight:2026-11-22:total", "tonight:2026-11-22:user:" + f.user.ID.Hex(),
		"tonight:2026-11-23:total", "tonight:2026-11-23:user:" + f.user.ID.Hex(),
	}
	if got := f.store.UsageKeys(); len(got) != 4 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Errorf("counters = %v, want %v", got, want)
	}
}

func TestNoBudgetMeansNoCalls(t *testing.T) {
	for _, limits := range []Limits{{PerUser: 0, Total: 200}, {PerUser: 10, Total: 0}, {PerUser: -1, Total: -1}} {
		f := newFixture(t, limits)
		f.addMany("Title", 5)
		_, err := f.tonight(Request{})
		var limitErr *LimitError
		if !errors.As(err, &limitErr) || f.model.called() != 0 {
			t.Errorf("limits %+v: error = %v after %d calls; want a refusal and no call", limits, err, f.model.called())
		}
	}
}

// Many requests at once, from many users, against a small shared budget:
// exactly that many calls are made and paid for, and every other request is
// refused before a call.
func TestTotalLimitHoldsUnderConcurrency(t *testing.T) {
	const (
		users = 40
		total = 7
	)
	f := newFixture(t, Limits{PerUser: 10, Total: total})
	everyone := make([]*models.User, users)
	for i := range everyone {
		everyone[i] = f.addUser(string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@example.com")
		for _, name := range []string{"A", "B", "C", "D"} {
			f.add(title{name: name, user: everyone[i]})
		}
	}

	var served, refused atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, user := range everyone {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.rec.Tonight(context.Background(), user, Request{})
			var limitErr *LimitError
			switch {
			case err == nil:
				served.Add(1)
			case errors.As(err, &limitErr) && limitErr.Scope == ScopeTotal:
				refused.Add(1)
			default:
				t.Errorf("Tonight: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if served.Load() != total || refused.Load() != users-total {
		t.Errorf("%d served and %d refused, want %d and %d", served.Load(), refused.Load(), total, users-total)
	}
	if f.model.called() != total {
		t.Errorf("the model was called %d times, want exactly the limit of %d", f.model.called(), total)
	}
	if used := f.store.Usage(totalKey); used != total {
		t.Errorf("total count = %d, want %d", used, total)
	}
	// Every refused user got their own place back.
	held := 0
	for _, user := range everyone {
		held += f.store.Usage(userKeyPrefix + user.ID.Hex())
	}
	if held != total {
		t.Errorf("users hold %d places between them, want %d: one per call made", held, total)
	}
}

// The same user, from many tabs at once.
func TestPerUserLimitHoldsUnderConcurrency(t *testing.T) {
	const (
		requests = 30
		perUser  = 4
	)
	f := newFixture(t, Limits{PerUser: perUser, Total: 200})
	f.addMany("Title", 6)

	var served, refused atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.tonight(Request{})
			var limitErr *LimitError
			switch {
			case err == nil:
				served.Add(1)
			case errors.As(err, &limitErr) && limitErr.Scope == ScopeUser:
				refused.Add(1)
			default:
				t.Errorf("Tonight: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if served.Load() != perUser || refused.Load() != requests-perUser {
		t.Errorf("%d served and %d refused, want %d and %d", served.Load(), refused.Load(), perUser, requests-perUser)
	}
	if f.model.called() != perUser {
		t.Errorf("the model was called %d times, want exactly the limit of %d", f.model.called(), perUser)
	}
	if user, total := f.store.Usage(userKeyPrefix+f.user.ID.Hex()), f.store.Usage(totalKey); user != perUser || total != perUser {
		t.Errorf("counts = %d for the user and %d in total, want %d and %d", user, total, perUser, perUser)
	}
}

// If the place cannot be handed back the request is still refused, and the
// error is still the limit: the user is one call short, never one over.
func TestTotalLimitWhenTheReleaseFails(t *testing.T) {
	f := newFixture(t, Limits{PerUser: 10, Total: 1})
	f.addMany("Title", 5)
	f.mustTonight(Request{})
	f.store.Fail["ReleaseUsage"] = errors.New("database is down")

	_, err := f.tonight(Request{})
	wantLimit(t, err, ScopeTotal)
	if f.model.called() != 1 {
		t.Errorf("the model was called %d times, want 1", f.model.called())
	}
}

func TestCountersOutliveTheirDayByOne(t *testing.T) {
	q := quota{now: func() time.Time { return noon }}
	d := q.today(models.User{}.ID)
	if !d.resetsAt.Equal(midnight) || !d.expiresAt.Equal(midnight.AddDate(0, 0, 1)) {
		t.Errorf("resets at %s and expires at %s; want %s and a day later", d.resetsAt, d.expiresAt, midnight)
	}
}
