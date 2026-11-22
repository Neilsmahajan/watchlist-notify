package imdbsync

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	// DefaultConcurrency bounds parallel TMDb lookups. TMDb allows roughly 50
	// requests per second; 8 in flight stays far below that.
	DefaultConcurrency = 8
	// DefaultLookupTimeout bounds one TMDb lookup, so a single rate-limited
	// request cannot spend the whole run on the client's retry schedule.
	DefaultLookupTimeout = 8 * time.Second
)

// Store is the persistence a reconcile needs. database.Service satisfies it.
type Store interface {
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	GetTitlesByIMDbIDs(ctx context.Context, imdbIDs []string) (map[string]*models.Title, error)
	CreateWatchlistItems(ctx context.Context, items []*models.WatchlistItem) ([]*models.WatchlistItem, error)
	SetWatchlistItemIMDbID(ctx context.Context, userID, itemID primitive.ObjectID, imdbID string) (bool, error)
	MarkWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID, since time.Time) (bool, error)
	ClearWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID) (bool, error)
	DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince time.Time) (bool, error)
}

// Finder is the part of the TMDb client that maps an IMDb ID to a title.
type Finder interface {
	FindByExternalID(ctx context.Context, externalID, source string) (*tmdb.Result, error)
}

// Reconciler gathers what BuildPlan needs and applies the plan.
type Reconciler struct {
	store         Store
	finder        Finder
	now           func() time.Time
	concurrency   int
	lookupTimeout time.Duration
}

// Option configures a Reconciler.
type Option func(*Reconciler)

// WithClock replaces the source of the current time.
func WithClock(now func() time.Time) Option {
	return func(r *Reconciler) { r.now = now }
}

// WithLookupTimeout bounds each TMDb lookup.
func WithLookupTimeout(d time.Duration) Option {
	return func(r *Reconciler) {
		if d > 0 {
			r.lookupTimeout = d
		}
	}
}

// New returns a Reconciler. finder may be nil when no TMDb credentials are
// configured; CanResolve then reports false and every ID that is not already
// stored stays pending.
func New(store Store, finder Finder, opts ...Option) *Reconciler {
	r := &Reconciler{
		store:         store,
		finder:        finder,
		now:           time.Now,
		concurrency:   DefaultConcurrency,
		lookupTimeout: DefaultLookupTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// CanResolve reports whether IMDb IDs can be looked up on TMDb.
func (r *Reconciler) CanResolve() bool {
	return r != nil && r.finder != nil
}

// Request is one reconcile.
type Request struct {
	UserID primitive.ObjectID
	Mode   Mode
	// IDs are the incoming IMDb title IDs in list order.
	IDs []string
	// Complete says IDs is the whole of the linked list (see Input.Complete).
	Complete bool
	// LookupBudget, when positive, bounds the time spent asking TMDb. IDs
	// not resolved in time stay pending; what was resolved is still applied.
	LookupBudget time.Duration
	// DryRun stops after planning: nothing is written.
	DryRun bool
}

// Result says what a reconcile did. After a dry run it says what the plan
// would do, as if every write had succeeded.
type Result struct {
	// Plan is the plan as decided, before any write.
	Plan Plan
	// Outcomes has one entry per distinct incoming ID, after the writes.
	Outcomes map[string]Outcome

	// Total is the number of distinct incoming IDs. Added, Existing,
	// Unmatched and Pending add up to it.
	Total     int
	Added     int
	Existing  int
	Unmatched int
	Pending   int

	// Filled counts hand-added items that got their IMDb ID.
	Filled int
	// MarkedMissing, Unmarked and Removed count the sync's three writes on
	// items that left, came back to, or stayed off the list.
	MarkedMissing int
	Unmarked      int
	Removed       int

	// Lookups counts TMDb requests made.
	Lookups int
	// AddedTitles are the titles of the items that were created, for storing
	// their availability baselines.
	AddedTitles []models.TitleRef
}

// Run reconciles a user's watchlist with the incoming IDs.
//
// An error means the reconcile stopped early: before planning when the
// watchlist or the stored titles could not be read, or part-way through the
// writes. The Result is valid either way and counts what was actually done.
// Writes run in the order additions, IMDb ID fills, unmarks, marks, removals,
// and stop at the first failure, so nothing is ever removed after a write
// went wrong.
func (r *Reconciler) Run(ctx context.Context, req Request) (Result, error) {
	existing, err := r.store.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: req.UserID})
	if err != nil {
		return Result{}, fmt.Errorf("list watchlist: %w", err)
	}

	resolved, unmatched, lookups, err := r.resolve(ctx, unknownIDs(existing, req.IDs), req.LookupBudget)
	if err != nil {
		return Result{Lookups: lookups}, err
	}

	plan := BuildPlan(Input{
		Mode:      req.Mode,
		Now:       r.now().UTC(),
		Existing:  existing,
		IDs:       req.IDs,
		Complete:  req.Complete,
		Resolved:  resolved,
		Unmatched: unmatched,
	})
	res := Result{
		Plan:      plan,
		Outcomes:  make(map[string]Outcome, len(plan.Outcomes)),
		Total:     plan.Total,
		Existing:  plan.Existing,
		Unmatched: plan.Unmatched,
		Pending:   plan.Pending,
		Lookups:   lookups,
	}
	for id, outcome := range plan.Outcomes {
		res.Outcomes[id] = outcome
	}

	if req.DryRun {
		res.Added = len(plan.Add)
		res.Filled = len(plan.Fill)
		res.Unmarked = len(plan.ClearMissing)
		res.MarkedMissing = len(plan.MarkMissing)
		res.Removed = len(plan.Remove)
		return res, nil
	}
	err = r.apply(ctx, req.UserID, plan, &res)
	return res, err
}

// unknownIDs returns the distinct incoming IDs the user has no item for, in
// list order. Only these need resolving: an ID the user already has costs no
// lookup at all.
func unknownIDs(existing []*models.WatchlistItem, ids []string) []string {
	known := make(map[string]bool, len(existing)+len(ids))
	for _, item := range existing {
		if item.IMDbID != "" {
			known[item.IMDbID] = true
		}
	}
	var unknown []string
	for _, id := range ids {
		if !known[id] {
			known[id] = true
			unknown = append(unknown, id)
		}
	}
	return unknown
}

// resolve maps IMDb IDs to TMDb titles: first from the titles this service
// already stores, which costs one query, then by asking TMDb about the rest
// with bounded concurrency. IDs it returns in neither map are pending.
//
// A failed or timed-out lookup is not an error; the ID is simply pending.
func (r *Reconciler) resolve(ctx context.Context, ids []string, budget time.Duration) (resolved map[string]Target, unmatched map[string]bool, lookups int, err error) {
	resolved = make(map[string]Target, len(ids))
	unmatched = make(map[string]bool)
	if len(ids) == 0 {
		return resolved, unmatched, 0, nil
	}

	stored, err := r.store.GetTitlesByIMDbIDs(ctx, ids)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("load stored titles: %w", err)
	}
	var ask []string
	for _, id := range ids {
		title := stored[id]
		if title == nil || !validRef(title.Ref()) {
			ask = append(ask, id)
			continue
		}
		resolved[id] = Target{Ref: title.Ref(), Title: title.Title, Year: title.Year}
	}
	if len(ask) == 0 || r.finder == nil {
		return resolved, unmatched, 0, nil
	}

	if budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	var mu sync.Mutex
	forEachLimit(ctx, r.concurrency, ask, func(id string) {
		lookupCtx, cancel := context.WithTimeout(ctx, r.lookupTimeout)
		defer cancel()
		found, err := r.finder.FindByExternalID(lookupCtx, id, tmdb.ExternalSourceIMDbID)

		mu.Lock()
		defer mu.Unlock()
		lookups++
		if err != nil {
			return // pending
		}
		target, ok := targetFrom(found)
		if !ok {
			unmatched[id] = true
			return
		}
		resolved[id] = target
	})
	return resolved, unmatched, lookups, nil
}

// targetFrom converts a TMDb find result. ok is false when TMDb has no match
// or matched something that is neither a movie nor a show.
func targetFrom(found *tmdb.Result) (Target, bool) {
	if found == nil {
		return Target{}, false
	}
	mediaType, ok := models.TMDbMediaType(found.Type)
	ref := models.TitleRef{MediaType: mediaType, TMDbID: found.TMDBID}
	if !ok || !validRef(ref) {
		return Target{}, false
	}
	return Target{Ref: ref, Title: found.Title, Year: found.Year}, true
}

func validRef(ref models.TitleRef) bool {
	return ref.TMDbID > 0 && (ref.MediaType == models.MediaTypeMovie || ref.MediaType == models.MediaTypeTV)
}

// itemType converts a TMDb media type to a watchlist item type.
func itemType(mediaType string) string {
	if mediaType == models.MediaTypeTV {
		return models.WatchlistTypeShow
	}
	return models.WatchlistTypeMovie
}

// apply performs the plan's writes and records what happened in res. Every
// write is conditional in the store, so a write that no longer applies
// (the user changed or deleted the item a moment ago) is skipped, not forced.
func (r *Reconciler) apply(ctx context.Context, userID primitive.ObjectID, plan Plan, res *Result) error {
	if err := r.applyAdditions(ctx, userID, plan, res); err != nil {
		return err
	}
	for _, fill := range plan.Fill {
		done, err := r.store.SetWatchlistItemIMDbID(ctx, userID, fill.ItemID, fill.IMDbID)
		if err != nil {
			return fmt.Errorf("set imdb id: %w", err)
		}
		if done {
			res.Filled++
		}
	}
	for _, itemID := range plan.ClearMissing {
		done, err := r.store.ClearWatchlistItemIMDbMissing(ctx, userID, itemID)
		if err != nil {
			return fmt.Errorf("clear missing mark: %w", err)
		}
		if done {
			res.Unmarked++
		}
	}
	now := r.now().UTC()
	for _, itemID := range plan.MarkMissing {
		done, err := r.store.MarkWatchlistItemIMDbMissing(ctx, userID, itemID, now)
		if err != nil {
			return fmt.Errorf("mark missing: %w", err)
		}
		if done {
			res.MarkedMissing++
		}
	}
	for _, removal := range plan.Remove {
		done, err := r.store.DeleteSyncedWatchlistItem(ctx, userID, removal.ItemID, removal.MissingSince)
		if err != nil {
			return fmt.Errorf("remove item: %w", err)
		}
		if done {
			res.Removed++
		}
	}
	return nil
}

// applyAdditions inserts the planned items in one batch. An item the store
// refuses as a duplicate is one the user already has (they added it while
// this run was resolving titles): it counts as existing, never as a failure.
func (r *Reconciler) applyAdditions(ctx context.Context, userID primitive.ObjectID, plan Plan, res *Result) error {
	if len(plan.Add) == 0 {
		return nil
	}
	items := make([]*models.WatchlistItem, len(plan.Add))
	for i, add := range plan.Add {
		title := strings.TrimSpace(add.Target.Title)
		if title == "" {
			title = add.IMDbID
		}
		items[i] = &models.WatchlistItem{
			UserID: userID,
			Title:  title,
			Type:   itemType(add.Target.Ref.MediaType),
			Year:   add.Target.Year,
			TMDbID: add.Target.Ref.TMDbID,
			IMDbID: add.IMDbID,
			Status: models.WatchlistStatusPlanned,
			Source: plan.Mode.source(),
		}
	}

	created, err := r.store.CreateWatchlistItems(ctx, items)
	inserted := make(map[string]bool, len(created))
	for _, item := range created {
		inserted[item.IMDbID] = true
		if ref, ok := item.TitleRef(); ok {
			res.AddedTitles = append(res.AddedTitles, ref)
		}
	}
	for _, add := range plan.Add {
		switch {
		case inserted[add.IMDbID]:
			res.Added++
		case err == nil:
			res.Outcomes[add.IMDbID] = OutcomeExisting
			res.Existing++
		default:
			// The batch failed part-way; whether this one is stored is not
			// known, so it is neither added nor existing.
			res.Outcomes[add.IMDbID] = OutcomePending
			res.Pending++
		}
	}
	if err != nil {
		return fmt.Errorf("add items: %w", err)
	}
	return nil
}

// forEachLimit calls fn for every item with at most limit calls in flight.
// Once ctx is done no further calls start; it returns when the started ones
// have finished.
func forEachLimit[T any](ctx context.Context, limit int, items []T, fn func(T)) {
	if limit < 1 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	defer wg.Wait()
	for _, item := range items {
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		if ctx.Err() != nil {
			<-sem
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(item)
		}()
	}
}
