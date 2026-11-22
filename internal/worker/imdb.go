package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// IMDbSyncMinGap is the least time between two fetches of one user's IMDb
// list by the worker. IMDb does not allow its data to be used freely (see
// package imdb), so a list is read once a day and no more: the job may be
// triggered as often as anyone likes, a user synced less than this long ago
// is left alone. It is under 24 hours so that a daily trigger that drifts by
// a few hours still syncs everyone.
const IMDbSyncMinGap = 20 * time.Hour

// Time. The job runs its tasks one after another (refresh, imdb, digest) and
// Cloud Run kills the whole job at its task timeout, so this task must not
// take the time the digest needs.
//
// The longest this task can run:
//
//	240 s  imdbTaskBudget: the last user is started just before it is up
//	  2 s  imdbPause before that user
//	120 s  imdbUserTimeout: that user's fetch, lookups and writes
//	 15 s  imdbWriteTimeout: storing that user's status
//	 45 s  imdbBaselineBudget: availability for the titles the run added
//	-----
//	422 s  imdbWorstCase, about 7 minutes
//
// The job is deployed with a task timeout of 1800 s (JobTaskTimeout, set in
// scripts/release-worker.sh). That leaves 1378 s, about 23 minutes, for the
// refresh before this task and the digest after it; the refresh takes a
// couple of minutes today. With the 540 s the job had before, a slow refresh
// and a worst-case sync left the digest nothing. A test checks that the
// script and these numbers still agree.
const (
	// JobTaskTimeout is the Cloud Run task timeout the worker job is
	// deployed with. scripts/release-worker.sh sets it; this constant only
	// states it, for the sums above and the test that checks them.
	JobTaskTimeout = 30 * time.Minute
	// imdbWorstCase is the longest the task can run (see above).
	imdbWorstCase = imdbTaskBudget + imdbPause + imdbUserTimeout + imdbWriteTimeout + imdbBaselineBudget
	// jobTimeLeftForOthers is the least the refresh and the digest must be
	// left with, together, when this task takes as long as it can.
	jobTimeLeftForOthers = 20 * time.Minute
)

const (
	// imdbTaskBudget is how long a run keeps starting new users. Users it
	// does not get to are not claimed: the next run starts with whoever has
	// waited longest.
	imdbTaskBudget = 4 * time.Minute
	// One user's sync may take this long in all, of which this much reading
	// the list and this much asking TMDb. The rest is left for the writes.
	// A daily sync needs seconds: the list is a page or two, and only titles
	// nobody has listed before are asked of TMDb.
	imdbUserTimeout  = 2 * time.Minute
	imdbFetchTimeout = 45 * time.Second
	imdbLookupBudget = 60 * time.Second
	// imdbPause separates two users' fetches. Together with syncing users
	// one at a time it keeps the load on IMDb at one slow, steady reader.
	imdbPause = 2 * time.Second
	// imdbBaselineBudget bounds storing availability for newly added titles.
	imdbBaselineBudget = 45 * time.Second
	imdbWriteTimeout   = 15 * time.Second
)

// ParseIMDbSyncMode reads an IMDB_SYNC_MODE value. Empty means "apply";
// "dry-run" plans every sync and writes nothing. Anything else is an error
// rather than a guess.
func ParseIMDbSyncMode(value string) (dryRun bool, err error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "apply":
		return false, nil
	case "dry-run", "dryrun", "dry_run":
		return true, nil
	default:
		return false, fmt.Errorf("unknown IMDb sync mode %q (want %q or %q)", value, "apply", "dry-run")
	}
}

// IMDbStore is the persistence the IMDb task needs besides the reconcile's.
type IMDbStore interface {
	ListIMDbLinkedUsers(ctx context.Context, attemptedBefore time.Time) ([]*models.User, error)
	ClaimIMDbAttempt(ctx context.Context, userID primitive.ObjectID, now time.Time, minGap time.Duration) (bool, error)
	UpdateIMDbSync(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) (bool, error)
	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
}

// ListFetcher reads an IMDb list; *imdb.Client does.
type ListFetcher interface {
	FetchList(ctx context.Context, sourceID string) (imdb.List, error)
}

// ListSyncer reconciles a fetched list with a watchlist; *imdbsync.Reconciler does.
type ListSyncer interface {
	SyncList(ctx context.Context, req imdbsync.SyncRequest) imdbsync.SyncResult
}

// BaselineStorer stores availability for titles that have none yet;
// *availability.Refresher does.
type BaselineStorer interface {
	Ensure(ctx context.Context, refs []models.TitleRef, stored map[string]*models.Title, region string) map[string]*models.Title
}

// IMDbTask keeps the watchlists of users who linked an IMDb list in step
// with that list.
type IMDbTask struct {
	Store   IMDbStore
	Fetcher ListFetcher
	Syncer  ListSyncer
	// Baselines may be nil; availability for added titles is then left to
	// the next refresh.
	Baselines     BaselineStorer
	DefaultRegion string
	// DryRun logs what each sync would do and writes nothing at all: no
	// items, no status, not even the record that a fetch happened.
	DryRun bool
	// Budget is how long a run keeps starting new users; zero means
	// imdbTaskBudget.
	Budget time.Duration
	Logger *slog.Logger

	// Now and Sleep are replaceable in tests.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration)
}

// imdbSummary counts what one run did. It holds no user data.
type imdbSummary struct {
	users         int // linked and due
	synced        int // whole list read and applied
	partial       int // some of the list read, or some of it not applied yet
	failed        int // IMDb or the database could not be reached
	notFound      int // list gone or made private
	skipped       int // another run got there first, or this one was cancelled
	deferred      int // left for the next run because this one ran out of time
	outOfTime     int // of the partial ones: lease over before every item was adopted, marked or removed
	added         int
	adopted       int // imported items the sync took over
	removed       int
	markedMissing int
	unmatched     int
}

// Run syncs every linked user whose last fetch is at least IMDbSyncMinGap
// old, one after another, starting with whoever has waited longest. A user
// whose sync fails is counted and logged, and the run moves on to the next;
// the task itself fails only when the users could not be listed or not one
// of them could be synced. A run that uses up its budget leaves the remaining
// users, unclaimed, for the next run.
//
// The log carries user IDs and counts, never email addresses or titles.
func (t *IMDbTask) Run(ctx context.Context) error {
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := t.Now
	if now == nil {
		now = time.Now
	}
	sleep := t.Sleep
	if sleep == nil {
		sleep = sleepContext
	}

	budget := t.Budget
	if budget <= 0 {
		budget = imdbTaskBudget
	}
	started := now()

	users, err := t.Store.ListIMDbLinkedUsers(ctx, started.Add(-IMDbSyncMinGap))
	if err != nil {
		return fmt.Errorf("list linked users: %w", err)
	}

	sum := imdbSummary{users: len(users)}
	addedByRegion := make(map[string][]models.TitleRef)
	fetched := false
	for _, user := range users {
		if ctx.Err() != nil {
			sum.skipped++
			continue
		}
		if user.IMDbSync == nil {
			sum.skipped++
			continue
		}
		if now().Sub(started) >= budget {
			sum.deferred++
			continue
		}
		// Before the claim, not after it: the claim starts the 45 seconds
		// in which this user's items may be adopted, marked and removed
		// (imdbsync.OwnershipLease), and waiting must not use them up.
		if fetched {
			sleep(ctx, imdbPause)
		}
		// The time of the claim is the time of this user's run: it goes on
		// the status and on every mark the run makes, and the run's lease on
		// the user's items is counted from it (imdbsync.SyncRequest.At).
		claimedAt := now()
		if !t.DryRun {
			// The claim is what enforces one fetch a day: it succeeds only
			// if nobody fetched this user's list within the gap, whether
			// that was another run of this job or the user linking it.
			claimed, err := t.Store.ClaimIMDbAttempt(ctx, user.ID, claimedAt, IMDbSyncMinGap)
			if err != nil {
				sum.failed++
				logger.Warn("imdb: claim failed", "user_id", user.ID.Hex(), "error", err.Error())
				continue
			}
			if !claimed {
				sum.skipped++
				continue
			}
		}
		fetched = true

		out := t.syncUser(ctx, user, claimedAt)
		switch out.State.LastStatus {
		case models.IMDbStatusOK:
			sum.synced++
		case models.IMDbStatusPartial:
			sum.partial++
		case models.IMDbStatusNotFound:
			sum.notFound++
		default:
			sum.failed++
		}
		if out.Result.LeaseExpired {
			sum.outOfTime++
		}
		sum.added += out.Result.Added
		sum.adopted += out.Result.Adopted
		sum.removed += out.Result.Removed
		sum.markedMissing += out.Result.MarkedMissing
		sum.unmatched += out.Result.Unmatched
		if len(out.Result.AddedTitles) > 0 && !t.DryRun {
			region := user.RegionOr(t.DefaultRegion)
			addedByRegion[region] = append(addedByRegion[region], out.Result.AddedTitles...)
		}

		logger.Info("imdb: user synced",
			"user_id", user.ID.Hex(),
			"dry_run", t.DryRun,
			"status", out.State.LastStatus,
			"total", out.Result.Total,
			"added", out.Result.Added,
			"existing", out.Result.Existing,
			"adopted", out.Result.Adopted,
			"unmatched", out.Result.Unmatched,
			"pending", out.Result.Pending,
			"marked_missing", out.Result.MarkedMissing,
			"unmarked", out.Result.Unmarked,
			"removed", out.Result.Removed,
			"prune", string(out.Result.Plan.Prune),
			"link_changed", out.Result.LinkChanged,
			"lease_expired", out.Result.LeaseExpired,
			"tmdb_lookups", out.Result.Lookups,
		)
	}

	t.storeBaselines(ctx, logger, addedByRegion)

	logger.Info("imdb summary",
		"dry_run", t.DryRun,
		"users", sum.users,
		"synced", sum.synced,
		"partial", sum.partial,
		"failed", sum.failed,
		"not_found", sum.notFound,
		"skipped", sum.skipped,
		"deferred", sum.deferred,
		"out_of_time", sum.outOfTime,
		"added", sum.added,
		"adopted", sum.adopted,
		"removed", sum.removed,
		"marked_missing", sum.markedMissing,
		"unmatched", sum.unmatched,
	)

	if attempted := sum.synced + sum.partial + sum.failed + sum.notFound; sum.failed > 0 && sum.failed == attempted {
		return errors.New("imdb: no list could be synced")
	}
	return nil
}

// syncUser fetches one user's list, reconciles it and stores the outcome on
// the user. It never returns an error: what went wrong is in the state.
//
// The run is for the link in user, as it was when the users were listed. The
// user may have unlinked, or linked afresh, between that and the claim; from
// the claim on they cannot for a minute (imdbsync.LinkLockout), and the run
// adopts, marks and removes only in the first 45 seconds of that minute
// (imdbsync.OwnershipLease), after reading the link again. A fetch and
// lookups that together take longer than that leave those three for the
// next run; what is new on the list is added all the same. The outcome is
// stored only on the link the run was for.
func (t *IMDbTask) syncUser(ctx context.Context, user *models.User, claimedAt time.Time) imdbsync.SyncResult {
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	userCtx, cancel := context.WithTimeout(ctx, imdbUserTimeout)
	defer cancel()

	fetchCtx, cancelFetch := context.WithTimeout(userCtx, imdbFetchTimeout)
	list, fetchErr := t.Fetcher.FetchList(fetchCtx, user.IMDbSync.SourceID)
	cancelFetch()
	if fetchErr != nil {
		// The client's errors name a status or an error code, never a URL.
		logger.Warn("imdb: fetch failed", "user_id", user.ID.Hex(), "read", len(list.Entries), "error", fetchErr.Error())
	}

	out := t.Syncer.SyncList(userCtx, imdbsync.SyncRequest{
		UserID:       user.ID,
		Previous:     user.IMDbSync,
		SourceID:     user.IMDbSync.SourceID,
		Mode:         imdbsync.ModeSync,
		At:           claimedAt,
		List:         list,
		FetchErr:     fetchErr,
		LookupBudget: imdbLookupBudget,
		DryRun:       t.DryRun,
	})
	if out.Err != nil {
		logger.Warn("imdb: reconcile stopped early", "user_id", user.ID.Hex(), "error", out.Err.Error())
	}
	if out.Result.LinkChanged {
		logger.Info("imdb: link changed during sync; nothing adopted, marked or removed", "user_id", user.ID.Hex())
	}
	if out.Result.LeaseExpired {
		logger.Info("imdb: out of time; adopting, marking and removing continue at the next sync", "user_id", user.ID.Hex())
	}
	if t.DryRun {
		return out
	}

	// Its own deadline, so the outcome is recorded even when the sync used
	// up its time. The parent is ctx, not userCtx, for the same reason.
	writeCtx, cancelWrite := context.WithTimeout(context.WithoutCancel(ctx), imdbWriteTimeout)
	defer cancelWrite()
	stored, err := t.Store.UpdateIMDbSync(writeCtx, user.ID, out.State)
	switch {
	case err != nil:
		logger.Warn("imdb: status not stored", "user_id", user.ID.Hex(), "error", err.Error())
	case !stored:
		// The user unlinked or linked afresh while this ran. Their choice
		// stands; this run's status describes a link that is gone.
		logger.Info("imdb: link changed during sync; status dropped", "user_id", user.ID.Hex())
	}
	return out
}

// storeBaselines fetches availability for the titles this run added, so the
// items show it straight away and the next refresh has something to compare
// against. Best effort: what does not fit is picked up by the refresh.
func (t *IMDbTask) storeBaselines(ctx context.Context, logger *slog.Logger, byRegion map[string][]models.TitleRef) {
	if t.Baselines == nil || len(byRegion) == 0 || ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, imdbBaselineBudget)
	defer cancel()

	for region, refs := range byRegion {
		seen := make(map[string]bool, len(refs))
		var distinct []models.TitleRef
		var keys []string
		for _, ref := range refs {
			if !seen[ref.Key()] {
				seen[ref.Key()] = true
				distinct = append(distinct, ref)
				keys = append(keys, ref.Key())
			}
		}
		stored, err := t.Store.GetTitles(ctx, keys, []string{region})
		if err != nil {
			logger.Warn("imdb: loading titles before baselines failed", "region", region, "error", err.Error())
			continue
		}
		resolved := t.Baselines.Ensure(ctx, distinct, stored, region)
		logger.Info("imdb: baselines stored", "region", region, "titles", len(distinct), "resolved", len(resolved))
	}
}

// sleepContext waits for d or until ctx is done.
func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
