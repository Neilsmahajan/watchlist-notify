package availability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

const (
	// DefaultConcurrency bounds parallel TMDb requests. TMDb allows roughly
	// 50 requests per second; 8 in flight stays far below that.
	DefaultConcurrency = 8
	// MetadataMaxAge is how stale title metadata may get before it is rewritten.
	MetadataMaxAge = 7 * 24 * time.Hour
)

// ErrTitleNotFound means TMDb has no such title (the ID was deleted or merged).
var ErrTitleNotFound = errors.New("title not found on tmdb")

// Fetcher is the part of the TMDb client the refresher needs.
type Fetcher interface {
	GetTitle(ctx context.Context, mediaType string, id int) (*tmdb.TitleDetails, error)
}

// Store is the persistence the refresher needs.
type Store interface {
	// GetTitles returns the stored titles for keys, keyed by title key, with
	// provider snapshots limited to regions.
	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
	// InsertAvailabilityEvents inserts events, skipping IDs that already
	// exist, and reports how many were new.
	InsertAvailabilityEvents(ctx context.Context, events []models.AvailabilityEvent) (int, error)
	// SaveTitleSnapshot replaces the region snapshot only if the stored one
	// still has version prevVersion (nil: only if there is none yet). It
	// reports whether the write was applied.
	SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prevVersion *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error)
	// MarkTitleNotFound records that TMDb has no such title.
	MarkTitleNotFound(ctx context.Context, ref models.TitleRef, at time.Time) error
}

// Refresher fetches titles from TMDb and reconciles them with the store.
type Refresher struct {
	fetcher     Fetcher
	store       Store
	logger      *slog.Logger
	now         func() time.Time
	concurrency int
	minAge      time.Duration
}

// Option configures a Refresher.
type Option func(*Refresher)

// WithMinAge makes RefreshAll leave alone titles that were checked less than
// d ago in every region. It lets the job run more often than availability
// needs refreshing (the digest wants an hourly look at who is due; provider
// data changes daily at most) and makes re-running after a partial failure
// cheap. Zero, the default, refreshes everything every time.
func WithMinAge(d time.Duration) Option {
	return func(r *Refresher) { r.minAge = d }
}

// NewRefresher returns a Refresher. fetcher may be nil when no TMDb
// credentials are configured; every refresh then fails softly.
func NewRefresher(fetcher Fetcher, store Store, logger *slog.Logger, opts ...Option) *Refresher {
	if logger == nil {
		logger = slog.Default()
	}
	r := &Refresher{
		fetcher:     fetcher,
		store:       store,
		logger:      logger,
		now:         time.Now,
		concurrency: DefaultConcurrency,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Result describes what refreshing one title did.
type Result struct {
	// Title is the title as it now stands for the refreshed regions.
	Title *models.Title
	// Baselines counts regions that received their first snapshot.
	Baselines int
	// Events counts availability events that were newly recorded.
	Events int
	// Superseded counts regions where another writer replaced the snapshot
	// first; this run's snapshot was dropped for them.
	Superseded int
}

// Refresh fetches one title and, for each region, records what changed since
// the stored snapshot and then replaces it. stored is the title as currently
// stored (nil when it has no document yet); the caller loads it so that many
// titles can be loaded in one query.
//
// The write order makes the job safe to retry and to overlap with itself:
//
//  1. Events are inserted first, under IDs derived from the version of the
//     snapshot they were diffed against. A crash after this step loses
//     nothing: the snapshot is unchanged, the next run diffs against the same
//     version, and re-inserting the same IDs is a no-op.
//  2. The snapshot is replaced with a compare-and-set on that version. Of two
//     overlapping runs only one wins; the loser's write is dropped, and its
//     events carry the same IDs as the winner's.
//
// Any fetch error returns before either step, so a TMDb outage can never be
// read as "every provider dropped the title".
func (r *Refresher) Refresh(ctx context.Context, ref models.TitleRef, stored *models.Title, regions []string) (Result, error) {
	if r.fetcher == nil {
		return Result{}, errors.New("tmdb client not configured")
	}
	if len(regions) == 0 {
		return Result{}, errors.New("no regions to refresh")
	}

	details, err := r.fetcher.GetTitle(ctx, ref.MediaType, ref.TMDbID)
	if err != nil {
		if errors.Is(err, tmdb.ErrNotFound) {
			if markErr := r.store.MarkTitleNotFound(ctx, ref, r.now().UTC()); markErr != nil {
				return Result{}, fmt.Errorf("mark %s not found: %w", ref.Key(), markErr)
			}
			return Result{}, ErrTitleNotFound
		}
		return Result{}, fmt.Errorf("fetch %s: %w", ref.Key(), err)
	}

	now := r.now().UTC().Truncate(time.Millisecond)
	title := cloneTitle(stored, ref, now)

	var meta *models.TitleMetadata
	if stored == nil || stored.MetadataFetchedAt == nil || now.Sub(*stored.MetadataFetchedAt) >= MetadataMaxAge {
		m := metadataFrom(details, now)
		meta = &m
		title.TitleMetadata = m
	}

	res := Result{Title: title}
	for _, region := range regions {
		// A region missing from a successful response means TMDb lists no
		// providers there, which BuildSnapshot turns into an empty snapshot.
		fresh := BuildSnapshot(details.Providers[region], now)

		var prev *models.RegionSnapshot
		var prevVersion *time.Time
		if snap, ok := stored.Snapshot(region); ok {
			prev = &snap
			prevVersion = &snap.FetchedAt
			if !fresh.FetchedAt.After(snap.FetchedAt) {
				// Versions must move forward for the compare-and-set to mean anything.
				fresh.FetchedAt = snap.FetchedAt.Add(time.Millisecond)
			}
		}

		if events := Events(ref, region, prev, fresh); len(events) > 0 {
			inserted, err := r.store.InsertAvailabilityEvents(ctx, events)
			if err != nil {
				return res, fmt.Errorf("record events for %s in %s: %w", ref.Key(), region, err)
			}
			res.Events += inserted
		}

		applied, err := r.store.SaveTitleSnapshot(ctx, ref, region, prevVersion, fresh, meta)
		if err != nil {
			return res, fmt.Errorf("save snapshot for %s in %s: %w", ref.Key(), region, err)
		}
		switch {
		case !applied:
			res.Superseded++
		case prev == nil:
			res.Baselines++
		}
		title.Providers[region] = fresh
	}
	return res, nil
}

// Peek fetches a title's snapshot for one region without storing anything.
// It serves availability lookups for regions that are not tracked.
func (r *Refresher) Peek(ctx context.Context, ref models.TitleRef, region string) (models.RegionSnapshot, error) {
	if r.fetcher == nil {
		return models.RegionSnapshot{}, errors.New("tmdb client not configured")
	}
	details, err := r.fetcher.GetTitle(ctx, ref.MediaType, ref.TMDbID)
	if err != nil {
		if errors.Is(err, tmdb.ErrNotFound) {
			return models.RegionSnapshot{}, ErrTitleNotFound
		}
		return models.RegionSnapshot{}, fmt.Errorf("fetch %s: %w", ref.Key(), err)
	}
	return BuildSnapshot(details.Providers[region], r.now()), nil
}

// CanFetch reports whether the refresher has a TMDb client to fetch with.
func (r *Refresher) CanFetch() bool {
	return r.fetcher != nil
}

// PeekAll is Peek for many titles with bounded concurrency. It returns the
// snapshots it could fetch before ctx ended, keyed by title key.
func (r *Refresher) PeekAll(ctx context.Context, refs []models.TitleRef, region string) map[string]models.RegionSnapshot {
	snapshots := make(map[string]models.RegionSnapshot, len(refs))
	if r.fetcher == nil {
		return snapshots
	}
	var mu sync.Mutex
	forEachLimit(ctx, r.concurrency, refs, func(ref models.TitleRef) {
		snap, err := r.Peek(ctx, ref, region)
		if err != nil {
			return
		}
		mu.Lock()
		snapshots[ref.Key()] = snap
		mu.Unlock()
	})
	return snapshots
}

// Ensure makes sure the given titles have a stored snapshot for region,
// fetching the ones that do not. It is the read path's miss handler and the
// way a newly added item gets its title row. stored holds whatever the caller
// already loaded for those titles (entries may be missing).
//
// Ensure is best effort and bounded by ctx: it returns the titles it managed
// to resolve, keyed by title key, and logs the rest. A title that already has
// a snapshot for the region is returned as is, without a fetch.
func (r *Refresher) Ensure(ctx context.Context, refs []models.TitleRef, stored map[string]*models.Title, region string) map[string]*models.Title {
	resolved := make(map[string]*models.Title, len(refs))
	var pending []models.TitleRef
	for _, ref := range refs {
		existing := stored[ref.Key()]
		if _, ok := existing.Snapshot(region); ok {
			resolved[ref.Key()] = existing
			continue
		}
		if existing != nil && existing.NotFoundAt != nil {
			continue // known missing on TMDb; the daily refresh re-checks it
		}
		pending = append(pending, ref)
	}
	if len(pending) == 0 || r.fetcher == nil {
		return resolved
	}

	var (
		mu     sync.Mutex
		failed int
	)
	forEachLimit(ctx, r.concurrency, pending, func(ref models.TitleRef) {
		res, err := r.Refresh(ctx, ref, stored[ref.Key()], []string{region})
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			if !errors.Is(err, ErrTitleNotFound) {
				failed++
			}
			return
		}
		resolved[ref.Key()] = res.Title
	})
	if unresolved := len(refs) - len(resolved); unresolved > 0 {
		r.logger.Warn("availability: titles left unresolved",
			"requested", len(pending), "unresolved", unresolved, "failed", failed, "region", region)
	}
	return resolved
}

// TrackedRegions returns the regions availability is stored for: every region
// a user is in plus the default one, normalised, without duplicates, sorted.
func TrackedRegions(userRegions []string, defaultRegion string) []string {
	seen := make(map[string]bool, len(userRegions)+1)
	var regions []string
	for _, raw := range append([]string{defaultRegion}, userRegions...) {
		region, ok := models.NormalizeRegion(raw)
		if !ok || seen[region] {
			continue
		}
		seen[region] = true
		regions = append(regions, region)
	}
	sort.Strings(regions)
	return regions
}

// Summary is the outcome of a full refresh run.
type Summary struct {
	Titles     int // distinct titles on any watchlist
	Fresh      int // left alone: checked more recently than the minimum age
	Refreshed  int // fetched and reconciled
	Baselines  int // region snapshots stored for the first time
	Changed    int // titles with at least one new event
	Events     int // events newly recorded
	NotFound   int // titles TMDb no longer has
	Superseded int // region snapshots another run replaced first
	Failed     int // fetch or store errors
	Skipped    int // not attempted because the run was cancelled
	Duration   time.Duration

	// NameMatched counts streaming providers that mapped to a service only
	// through the name fallback, and Unmapped the ones that mapped to nothing.
	// Both are keyed "Provider Name (id)" and exist so the mapping table can
	// be checked against what TMDb actually returns.
	NameMatched map[string]int
	Unmapped    map[string]int
}

// RefreshAll refreshes every given title for every region with bounded
// concurrency, skipping titles that are still fresh (see WithMinAge).
// Failures are counted and logged, never fatal: one bad title must not stop
// the run.
func (r *Refresher) RefreshAll(ctx context.Context, refs []models.TitleRef, regions []string) (Summary, error) {
	start := r.now()
	sum := Summary{
		Titles:      len(refs),
		NameMatched: map[string]int{},
		Unmapped:    map[string]int{},
	}
	if len(refs) == 0 {
		return sum, nil
	}

	keys := make([]string, len(refs))
	for i, ref := range refs {
		keys[i] = ref.Key()
	}
	stored, err := r.store.GetTitles(ctx, keys, regions)
	if err != nil {
		return sum, fmt.Errorf("load stored titles: %w", err)
	}

	due := refs
	if r.minAge > 0 {
		cutoff := start.Add(-r.minAge)
		due = make([]models.TitleRef, 0, len(refs))
		for _, ref := range refs {
			if !checkedSince(stored[ref.Key()], regions, cutoff) {
				due = append(due, ref)
			}
		}
		sum.Fresh = len(refs) - len(due)
	}

	var (
		mu        sync.Mutex
		attempted int
	)
	forEachLimit(ctx, r.concurrency, due, func(ref models.TitleRef) {
		res, err := r.Refresh(ctx, ref, stored[ref.Key()], regions)

		mu.Lock()
		defer mu.Unlock()
		attempted++
		switch {
		case errors.Is(err, ErrTitleNotFound):
			sum.NotFound++
			return
		case err != nil:
			sum.Failed++
			r.logger.Warn("refresh: title failed", "title", ref.Key(), "error", err.Error())
			return
		}
		sum.Refreshed++
		sum.Baselines += res.Baselines
		sum.Superseded += res.Superseded
		sum.Events += res.Events
		if res.Events > 0 {
			sum.Changed++
		}
		for _, region := range regions {
			snap, _ := res.Title.Snapshot(region)
			tallyMapping(&sum, snap)
		}
	})
	sum.Skipped = len(due) - attempted
	sum.Duration = r.now().Sub(start)
	return sum, nil
}

// checkedSince reports whether TMDb was asked about the title after cutoff
// for every region: it has a snapshot that recent for each, or was found
// missing that recently.
func checkedSince(title *models.Title, regions []string, cutoff time.Time) bool {
	if title == nil {
		return false
	}
	if title.NotFoundAt != nil {
		return title.NotFoundAt.After(cutoff)
	}
	for _, region := range regions {
		if snap, ok := title.Snapshot(region); !ok || !snap.FetchedAt.After(cutoff) {
			return false
		}
	}
	return true
}

func tallyMapping(sum *Summary, snap models.RegionSnapshot) {
	counted := make(map[int]bool)
	for _, offer := range snap.Offers {
		if !models.IsStreamingAccess(offer.Access) || counted[offer.ProviderID] {
			continue
		}
		counted[offer.ProviderID] = true
		label := fmt.Sprintf("%s (%d)", offer.ProviderName, offer.ProviderID)
		if _, mapped := offer.ServiceCode(); !mapped {
			sum.Unmapped[label]++
		} else if !models.MappedByID(offer.ProviderID) {
			sum.NameMatched[label]++
		}
	}
}

func metadataFrom(d *tmdb.TitleDetails, fetchedAt time.Time) models.TitleMetadata {
	return models.TitleMetadata{
		Title:             d.Title,
		Year:              d.Year,
		PosterPath:        d.PosterPath,
		IMDbID:            d.IMDbID,
		Runtime:           d.Runtime,
		Genres:            d.Genres,
		Rating:            d.Rating,
		VoteCount:         d.VoteCount,
		Overview:          d.Overview,
		MetadataFetchedAt: &fetchedAt,
	}
}

// cloneTitle returns a copy of stored that is safe to modify, or a fresh
// title when nothing is stored yet.
func cloneTitle(stored *models.Title, ref models.TitleRef, now time.Time) *models.Title {
	title := &models.Title{
		ID:        ref.Key(),
		MediaType: ref.MediaType,
		TMDbID:    ref.TMDbID,
		CreatedAt: now,
	}
	if stored != nil {
		*title = *stored
		title.NotFoundAt = nil
	}
	title.UpdatedAt = now
	providers := make(map[string]models.RegionSnapshot, len(title.Providers)+1)
	for region, snap := range title.Providers {
		providers[region] = snap
	}
	title.Providers = providers
	return title
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
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			fn(item)
		}()
	}
	wg.Wait()
}
