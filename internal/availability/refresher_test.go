package availability

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
)

var (
	matrix  = models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 603}
	thrones = models.TitleRef{MediaType: models.MediaTypeTV, TMDbID: 1399}
)

// fakeFetcher answers GetTitle from a table that tests rewrite between runs.
type fakeFetcher struct {
	mu        sync.Mutex
	responses map[string]*tmdb.TitleDetails
	errs      map[string]error
	calls     map[string]int
}

func newFakeFetcher() *fakeFetcher {
	return &fakeFetcher{
		responses: make(map[string]*tmdb.TitleDetails),
		errs:      make(map[string]error),
		calls:     make(map[string]int),
	}
}

func (f *fakeFetcher) GetTitle(ctx context.Context, mediaType string, id int) (*tmdb.TitleDetails, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := models.TitleRef{MediaType: mediaType, TMDbID: id}.Key()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[key]++
	if err := f.errs[key]; err != nil {
		return nil, err
	}
	if details, ok := f.responses[key]; ok {
		return details, nil
	}
	return nil, &tmdb.APIError{Op: "title", StatusCode: 404}
}

// total is how many title lookups the fake has served.
func (f *fakeFetcher) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		n += c
	}
	return n
}

// streams makes ref answer with the given providers streaming it in the US.
func (f *fakeFetcher) streams(ref models.TitleRef, providers ...tmdb.Provider) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.errs, ref.Key())
	f.responses[ref.Key()] = &tmdb.TitleDetails{
		ID:        ref.TMDbID,
		MediaType: ref.MediaType,
		Title:     "Title " + ref.Key(),
		Year:      1999,
		Runtime:   136,
		Rating:    8.2,
		Providers: map[string]tmdb.RegionProviders{
			"US": {Link: "https://example.test/watch", Flatrate: providers},
		},
	}
}

func (f *fakeFetcher) fails(ref models.TitleRef, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[ref.Key()] = err
}

var (
	netflix = tmdb.Provider{ProviderID: 8, ProviderName: "Netflix"}
	hulu    = tmdb.Provider{ProviderID: 15, ProviderName: "Hulu"}
)

// harness wires a refresher to a fake fetcher, an in-memory store and a clock
// that tests move by hand.
type harness struct {
	t       *testing.T
	fetcher *fakeFetcher
	store   *storetest.Store
	r       *Refresher
	clock   time.Time
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, fetcher: newFakeFetcher(), store: storetest.New(), clock: t0}
	h.r = NewRefresher(h.fetcher, h.store, nil)
	h.r.now = func() time.Time { return h.clock }
	return h
}

// refresh runs one refresh of ref against whatever is stored, a day after the
// previous one.
func (h *harness) refresh(ref models.TitleRef) (Result, error) {
	h.t.Helper()
	h.clock = h.clock.Add(24 * time.Hour)
	return h.r.Refresh(context.Background(), ref, h.store.Title(ref.Key()), []string{"US"})
}

func (h *harness) mustRefresh(ref models.TitleRef) Result {
	h.t.Helper()
	res, err := h.refresh(ref)
	if err != nil {
		h.t.Fatalf("Refresh(%s): %v", ref.Key(), err)
	}
	return res
}

func (h *harness) streamingCodes(ref models.TitleRef) []string {
	h.t.Helper()
	snap, ok := h.store.Title(ref.Key()).Snapshot("US")
	if !ok {
		h.t.Fatalf("%s has no stored US snapshot", ref.Key())
	}
	var codes []string
	for _, svc := range snap.Services() {
		codes = append(codes, svc.Code)
	}
	return codes
}

func summarize(events []models.AvailabilityEvent) []string {
	var out []string
	for _, ev := range events {
		out = append(out, ev.ServiceCode+" "+ev.Change)
	}
	return out
}

func TestRefreshBaselineRecordsNoEvents(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix, hulu)

	res := h.mustRefresh(matrix)

	if res.Baselines != 1 || res.Events != 0 {
		t.Errorf("result = %+v, want one baseline and no events", res)
	}
	if events := h.store.Events(); len(events) != 0 {
		t.Errorf("baseline recorded events: %v", summarize(events))
	}
	if got, want := h.streamingCodes(matrix), []string{"netflix", "hulu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stored services = %v, want %v", got, want)
	}
	title := h.store.Title(matrix.Key())
	if title.Title != "Title movie:603" || title.Runtime != 136 || title.MetadataFetchedAt == nil {
		t.Errorf("metadata not stored with the baseline: %+v", title.TitleMetadata)
	}
}

func TestRefreshRecordsAddedAndRemoved(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)

	h.fetcher.streams(matrix, hulu)
	res := h.mustRefresh(matrix)

	if res.Events != 2 || res.Baselines != 0 || res.Superseded != 0 {
		t.Errorf("result = %+v, want two events", res)
	}
	events := h.store.Events()
	if got, want := summarize(events), []string{"hulu added", "netflix removed"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	snap, _ := h.store.Title(matrix.Key()).Snapshot("US")
	for _, ev := range events {
		if !ev.DetectedAt.Equal(snap.FetchedAt) {
			t.Errorf("event detected_at = %v, want the new snapshot's time %v", ev.DetectedAt, snap.FetchedAt)
		}
		if got := ev.ExpiresAt.Sub(ev.DetectedAt); got != models.EventRetention {
			t.Errorf("event expires after %v, want %v", got, models.EventRetention)
		}
	}

	// Nothing changed since: no further events.
	if res := h.mustRefresh(matrix); res.Events != 0 {
		t.Errorf("unchanged refresh recorded %d events", res.Events)
	}
	if n := len(h.store.Events()); n != 2 {
		t.Errorf("store has %d events, want 2", n)
	}
}

// A title TMDb answers for, but with no providers in the region, has really
// left every service. That is different from a fetch that failed.
func TestRefreshEmptyRegionIsARemoval(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)

	h.fetcher.streams(matrix) // success, nothing streaming
	h.mustRefresh(matrix)

	if got, want := summarize(h.store.Events()), []string{"netflix removed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestRefreshFailedFetchChangesNothing(t *testing.T) {
	failures := map[string]error{
		"network error":            errors.New("dial tcp: connection refused"),
		"rate limited":             &tmdb.APIError{Op: "title", StatusCode: 429},
		"server error":             &tmdb.APIError{Op: "title", StatusCode: 503},
		"response lacks providers": tmdb.ErrNoProviderData,
	}
	for name, fetchErr := range failures {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.fetcher.streams(matrix, netflix, hulu)
			h.mustRefresh(matrix)
			before, writes := h.store.Title(matrix.Key()), h.store.Writes

			h.fetcher.fails(matrix, fetchErr)
			_, err := h.refresh(matrix)

			if !errors.Is(err, fetchErr) {
				t.Fatalf("Refresh error = %v, want it to wrap %v", err, fetchErr)
			}
			if h.store.Writes != writes {
				t.Errorf("a failed fetch wrote to the store %d times", h.store.Writes-writes)
			}
			if len(h.store.Events()) != 0 {
				t.Errorf("a failed fetch recorded events: %v", summarize(h.store.Events()))
			}
			if after := h.store.Title(matrix.Key()); !reflect.DeepEqual(after, before) {
				t.Errorf("stored title changed after a failed fetch:\n got %+v\nwant %+v", after, before)
			}
		})
	}
}

func TestRefreshNotFoundKeepsSnapshot(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)

	h.fetcher.fails(matrix, &tmdb.APIError{Op: "title", StatusCode: 404})
	if _, err := h.refresh(matrix); !errors.Is(err, ErrTitleNotFound) {
		t.Fatalf("Refresh error = %v, want ErrTitleNotFound", err)
	}

	title := h.store.Title(matrix.Key())
	if title.NotFoundAt == nil {
		t.Error("title was not marked as not found")
	}
	if got, want := h.streamingCodes(matrix), []string{"netflix"}; !reflect.DeepEqual(got, want) {
		t.Errorf("stored services = %v, want the last known %v", got, want)
	}
	if len(h.store.Events()) != 0 {
		t.Errorf("a missing title recorded events: %v", summarize(h.store.Events()))
	}

	// TMDb has it again: the mark is cleared.
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)
	if h.store.Title(matrix.Key()).NotFoundAt != nil {
		t.Error("not-found mark survived a successful refresh")
	}
}

// failingSaves makes the first n snapshot writes fail, as a crash or a
// database blip between recording events and replacing the snapshot would.
type failingSaves struct {
	*storetest.Store
	remaining int
}

func (s *failingSaves) SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prev *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error) {
	if s.remaining > 0 {
		s.remaining--
		return false, errors.New("write concern timeout")
	}
	return s.Store.SaveTitleSnapshot(ctx, ref, region, prev, snap, meta)
}

func TestRefreshRetryAfterPartialWriteDoesNotDuplicateEvents(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)

	// The next run records its events and then dies before the snapshot is
	// replaced.
	flaky := &failingSaves{Store: h.store, remaining: 1}
	h.r.store = flaky
	h.fetcher.streams(matrix, netflix, hulu)
	if _, err := h.refresh(matrix); err == nil {
		t.Fatal("Refresh succeeded although the snapshot write failed")
	}
	if got, want := summarize(h.store.Events()), []string{"hulu added"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events after the failed run = %v, want %v", got, want)
	}
	if got, want := h.streamingCodes(matrix), []string{"netflix"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot after the failed run = %v, want the old %v", got, want)
	}

	// The retry diffs against the same stored snapshot. It finds the same
	// change, under the same ID, and this time the snapshot moves on.
	res := h.mustRefresh(matrix)
	if res.Events != 0 {
		t.Errorf("retry reported %d new events, want 0", res.Events)
	}
	if got, want := summarize(h.store.Events()), []string{"hulu added"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events after the retry = %v, want %v", got, want)
	}
	if got, want := h.streamingCodes(matrix), []string{"netflix", "hulu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot after the retry = %v, want %v", got, want)
	}
}

func TestRefreshOverlappingRunsRecordAChangeOnce(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)

	// Two job runs load the same stored title, then both see Hulu arrive.
	stale := h.store.Title(matrix.Key())
	h.fetcher.streams(matrix, netflix, hulu)
	h.clock = h.clock.Add(24 * time.Hour)

	first, err := h.r.Refresh(context.Background(), matrix, stale, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	h.clock = h.clock.Add(time.Second)
	second, err := h.r.Refresh(context.Background(), matrix, stale, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}

	if first.Events != 1 || first.Superseded != 0 {
		t.Errorf("first run = %+v, want one event", first)
	}
	if second.Events != 0 || second.Superseded != 1 {
		t.Errorf("second run = %+v, want no new events and a superseded snapshot", second)
	}
	if got, want := summarize(h.store.Events()), []string{"hulu added"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestRefreshOverlappingBaselinesStoreOne(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)

	first, err := h.r.Refresh(context.Background(), matrix, nil, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.r.Refresh(context.Background(), matrix, nil, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Baselines != 1 || second.Baselines != 0 || second.Superseded != 1 {
		t.Errorf("first = %+v, second = %+v; want exactly one baseline", first, second)
	}
}

func TestRefreshRewritesMetadataOnlyWhenStale(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix)
	fetchedAt := *h.store.Title(matrix.Key()).MetadataFetchedAt

	// TMDb's rating drifts daily; it is not rewritten on every run.
	h.fetcher.responses[matrix.Key()].Rating = 8.3
	h.mustRefresh(matrix)
	if title := h.store.Title(matrix.Key()); title.Rating != 8.2 || !title.MetadataFetchedAt.Equal(fetchedAt) {
		t.Errorf("metadata rewritten after one day: rating %v, fetched %v", title.Rating, title.MetadataFetchedAt)
	}

	h.clock = h.clock.Add(MetadataMaxAge)
	h.mustRefresh(matrix)
	if title := h.store.Title(matrix.Key()); title.Rating != 8.3 || !title.MetadataFetchedAt.After(fetchedAt) {
		t.Errorf("stale metadata was not rewritten: rating %v, fetched %v", title.Rating, title.MetadataFetchedAt)
	}
}

func TestRefreshWithoutFetcher(t *testing.T) {
	r := NewRefresher(nil, storetest.New(), nil)
	if r.CanFetch() {
		t.Error("CanFetch() = true without a fetcher")
	}
	if _, err := r.Refresh(context.Background(), matrix, nil, []string{"US"}); err == nil {
		t.Error("Refresh succeeded without a fetcher")
	}
	if got := r.Ensure(context.Background(), []models.TitleRef{matrix}, nil, "US"); len(got) != 0 {
		t.Errorf("Ensure resolved %d titles without a fetcher", len(got))
	}
}

func TestRefreshAllCountsAndSurvivesFailures(t *testing.T) {
	h := newHarness(t)
	bad := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 1}
	gone := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 2}
	refs := []models.TitleRef{matrix, thrones, bad, gone}

	h.fetcher.streams(matrix, netflix)
	h.fetcher.streams(thrones, tmdb.Provider{ProviderID: 999030, ProviderName: "HBO Max"}, tmdb.Provider{ProviderID: 999020, ProviderName: "Kanopy"})
	h.fetcher.fails(bad, errors.New("boom"))
	// gone: the fake answers 404 for titles it does not know.

	sum, err := h.r.RefreshAll(context.Background(), refs, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Titles != 4 || sum.Refreshed != 2 || sum.Baselines != 2 || sum.Failed != 1 || sum.NotFound != 1 || sum.Events != 0 || sum.Skipped != 0 {
		t.Errorf("first run summary = %+v", sum)
	}
	if want := map[string]int{"HBO Max (999030)": 1}; !reflect.DeepEqual(sum.NameMatched, want) {
		t.Errorf("NameMatched = %v, want %v", sum.NameMatched, want)
	}
	if want := map[string]int{"Kanopy (999020)": 1}; !reflect.DeepEqual(sum.Unmapped, want) {
		t.Errorf("Unmapped = %v, want %v", sum.Unmapped, want)
	}

	h.clock = h.clock.Add(24 * time.Hour)
	h.fetcher.streams(matrix, netflix, hulu)
	sum, err = h.r.RefreshAll(context.Background(), refs, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Refreshed != 2 || sum.Baselines != 0 || sum.Changed != 1 || sum.Events != 1 || sum.Failed != 1 {
		t.Errorf("second run summary = %+v", sum)
	}
}

func TestRefreshAllSkipsFreshTitles(t *testing.T) {
	h := newHarness(t)
	h.r = NewRefresher(h.fetcher, h.store, nil, WithMinAge(20*time.Hour))
	h.r.now = func() time.Time { return h.clock }
	gone := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 2}
	refs := []models.TitleRef{matrix, thrones, gone}
	h.fetcher.streams(matrix, netflix)
	h.fetcher.streams(thrones, netflix)

	run := func() Summary {
		t.Helper()
		sum, err := h.r.RefreshAll(context.Background(), refs, []string{"US"})
		if err != nil {
			t.Fatal(err)
		}
		return sum
	}

	if sum := run(); sum.Fresh != 0 || sum.Refreshed != 2 || sum.NotFound != 1 {
		t.Fatalf("first run summary = %+v, want everything fetched", sum)
	}

	// An hour later nothing is due, not even the title TMDb does not have.
	h.clock = h.clock.Add(time.Hour)
	h.fetcher.streams(matrix, netflix, hulu)
	calls := h.fetcher.total()
	if sum := run(); sum.Fresh != 3 || sum.Refreshed != 0 || sum.NotFound != 0 || sum.Skipped != 0 {
		t.Errorf("run after an hour = %+v, want all three left alone", sum)
	}
	if h.fetcher.total() != calls {
		t.Errorf("fresh titles cost %d TMDb calls", h.fetcher.total()-calls)
	}

	// A title checked for one region only is due once another is tracked.
	sum, err := h.r.RefreshAll(context.Background(), []models.TitleRef{thrones}, []string{"GB", "US"})
	if err != nil || sum.Fresh != 0 || sum.Refreshed != 1 {
		t.Errorf("run with a new region = %+v, %v; want the title refreshed", sum, err)
	}

	// The next day everything is due again and the change is picked up.
	h.clock = h.clock.Add(23 * time.Hour)
	if sum := run(); sum.Fresh != 0 || sum.Refreshed != 2 || sum.NotFound != 1 || sum.Events != 1 {
		t.Errorf("run a day later = %+v, want everything fetched and one event", sum)
	}
}

func TestRefreshAllStopsWhenCancelled(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sum, err := h.r.RefreshAll(ctx, []models.TitleRef{matrix, thrones}, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Skipped != 2 || sum.Refreshed != 0 {
		t.Errorf("summary = %+v, want both titles skipped", sum)
	}
	if h.store.Writes != 0 {
		t.Errorf("a cancelled run wrote %d times", h.store.Writes)
	}
}

func TestEnsureFetchesOnlyWhatIsMissing(t *testing.T) {
	h := newHarness(t)
	gone := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 2}
	fresh := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 3}
	failing := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 4}

	h.fetcher.streams(matrix, netflix)
	h.mustRefresh(matrix) // already stored
	if _, err := h.refresh(gone); !errors.Is(err, ErrTitleNotFound) {
		t.Fatal(err)
	}
	h.fetcher.streams(fresh, hulu)
	h.fetcher.fails(failing, errors.New("boom"))
	calls := func(ref models.TitleRef) int { return h.fetcher.calls[ref.Key()] }
	before := map[models.TitleRef]int{matrix: calls(matrix), gone: calls(gone)}

	refs := []models.TitleRef{matrix, gone, fresh, failing}
	stored, err := h.store.GetTitles(context.Background(), []string{matrix.Key(), gone.Key(), fresh.Key(), failing.Key()}, []string{"US"})
	if err != nil {
		t.Fatal(err)
	}
	resolved := h.r.Ensure(context.Background(), refs, stored, "US")

	if len(resolved) != 2 || resolved[matrix.Key()] == nil || resolved[fresh.Key()] == nil {
		t.Errorf("Ensure resolved %d titles, want the stored and the newly fetched one", len(resolved))
	}
	if calls(matrix) != before[matrix] {
		t.Error("Ensure refetched a title that already had a snapshot")
	}
	if calls(gone) != before[gone] {
		t.Error("Ensure refetched a title known to be missing on TMDb")
	}
	if got, want := h.streamingCodes(fresh), []string{"hulu"}; !reflect.DeepEqual(got, want) {
		t.Errorf("newly ensured title stored %v, want %v", got, want)
	}
	if len(h.store.Events()) != 0 {
		t.Errorf("Ensure recorded events for baselines: %v", summarize(h.store.Events()))
	}
}

func TestPeekStoresNothing(t *testing.T) {
	h := newHarness(t)
	h.fetcher.streams(matrix, netflix)

	snap, err := h.r.Peek(context.Background(), matrix, "US")
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HasService(models.ServiceNetflix) {
		t.Errorf("Peek snapshot = %+v, want Netflix", snap)
	}
	if h.store.Writes != 0 || h.store.Title(matrix.Key()) != nil {
		t.Error("Peek wrote to the store")
	}
	if got := h.r.PeekAll(context.Background(), []models.TitleRef{matrix, thrones}, "US"); len(got) != 1 {
		t.Errorf("PeekAll returned %d snapshots, want 1 (the other title is unknown)", len(got))
	}
}

func TestTrackedRegions(t *testing.T) {
	got := TrackedRegions([]string{"gb", "US", "", "nowhere", "GB", "ca"}, "us")
	if want := []string{"CA", "GB", "US"}; !reflect.DeepEqual(got, want) {
		t.Errorf("TrackedRegions = %v, want %v", got, want)
	}
	if got := TrackedRegions(nil, "US"); !reflect.DeepEqual(got, []string{"US"}) {
		t.Errorf("TrackedRegions with no users = %v, want [US]", got)
	}
}
