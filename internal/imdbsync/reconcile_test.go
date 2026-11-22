package imdbsync

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// --- fakes ---

// fakeTMDb answers FindByExternalID from a table and records the calls.
type fakeTMDb struct {
	mu      sync.Mutex
	titles  map[string]tmdb.Result
	errs    map[string]error
	calls   []string
	delay   time.Duration
	active  int
	maxSeen int
}

func newFakeTMDb() *fakeTMDb {
	return &fakeTMDb{titles: map[string]tmdb.Result{}, errs: map[string]error{}}
}

// movie and show register what an IMDb ID resolves to.
func (f *fakeTMDb) movie(imdbID string, tmdbID int, title string) *fakeTMDb {
	f.titles[imdbID] = tmdb.Result{TMDBID: tmdbID, Title: title, Year: 1999, Type: "movie"}
	return f
}

func (f *fakeTMDb) show(imdbID string, tmdbID int, title string) *fakeTMDb {
	f.titles[imdbID] = tmdb.Result{TMDBID: tmdbID, Title: title, Year: 2011, Type: "tv"}
	return f
}

func (f *fakeTMDb) FindByExternalID(ctx context.Context, externalID, source string) (*tmdb.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, externalID)
	f.active++
	if f.active > f.maxSeen {
		f.maxSeen = f.active
	}
	delay := f.delay
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	if source != tmdb.ExternalSourceIMDbID {
		return nil, fmt.Errorf("unexpected source %q", source)
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.errs[externalID]; err != nil {
		return nil, err
	}
	if res, ok := f.titles[externalID]; ok {
		return &res, nil
	}
	return nil, nil
}

func (f *fakeTMDb) lookups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string{}, f.calls...)
	sort.Strings(out)
	return out
}

// hookedStore runs a hook just before a write, the way another request can
// land between a plan and its writes.
type hookedStore struct {
	*storetest.Store
	beforeCreate func()
	beforeDelete func()
	createErr    error
}

func (h *hookedStore) CreateWatchlistItems(ctx context.Context, items []*models.WatchlistItem) ([]*models.WatchlistItem, error) {
	if h.createErr != nil {
		return nil, h.createErr
	}
	if h.beforeCreate != nil {
		h.beforeCreate()
	}
	return h.Store.CreateWatchlistItems(ctx, items)
}

func (h *hookedStore) DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince time.Time) (bool, error) {
	if h.beforeDelete != nil {
		h.beforeDelete()
	}
	return h.Store.DeleteSyncedWatchlistItem(ctx, userID, itemID, missingSince)
}

// world is a store, a TMDb and a clock for one user.
type world struct {
	t     *testing.T
	store *hookedStore
	tmdb  *fakeTMDb
	now   time.Time
	user  primitive.ObjectID
	rec   *Reconciler
}

func newWorld(t *testing.T) *world {
	w := &world{
		t:     t,
		store: &hookedStore{Store: storetest.New()},
		tmdb:  newFakeTMDb(),
		now:   time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC),
	}
	w.user = w.store.AddUser(&models.User{Email: "viewer@example.com"}).ID
	w.rec = New(w.store, w.tmdb, WithClock(func() time.Time { return w.now }))
	return w
}

func (w *world) run(mode Mode, complete bool, ids ...string) Result {
	w.t.Helper()
	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: mode, IDs: ids, Complete: complete})
	if err != nil {
		w.t.Fatalf("Run: %v", err)
	}
	return res
}

// sync is one worker run over a complete fetch.
func (w *world) sync(ids ...string) Result {
	w.t.Helper()
	return w.run(ModeSync, true, ids...)
}

// titles returns the user's watchlist as "imdb id: source/status" lines,
// with "missing" appended for marked items.
func (w *world) titles() map[string]string {
	out := map[string]string{}
	for _, item := range w.store.Items(w.user) {
		line := item.Source + "/" + item.Status
		if item.IMDbMissingSince != nil {
			line += "/missing"
		}
		key := item.IMDbID
		if key == "" {
			key = item.Title
		}
		out[key] = line
	}
	return out
}

func (w *world) item(imdbID string) *models.WatchlistItem {
	for _, item := range w.store.Items(w.user) {
		if item.IMDbID == imdbID {
			return item
		}
	}
	w.t.Fatalf("no item with IMDb ID %s", imdbID)
	return nil
}

func (w *world) expect(want map[string]string) {
	w.t.Helper()
	if got := w.titles(); !reflect.DeepEqual(got, want) {
		w.t.Errorf("watchlist = %v\nwant %v", got, want)
	}
}

const (
	matrix   = "tt0133093"
	inceptn  = "tt1375666"
	thrones  = "tt0944947"
	obscure  = "tt9999991"
	syncedAs = "imdb/planned"
)

func standardTMDb(f *fakeTMDb) {
	f.movie(matrix, 603, "The Matrix").movie(inceptn, 27205, "Inception").show(thrones, 1399, "Game of Thrones")
}

// --- adding ---

func TestRunAddsAFreshList(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)

	res := w.run(ModeLink, true, matrix, inceptn, thrones, obscure)

	if got := [5]int{res.Total, res.Added, res.Existing, res.Unmatched, res.Pending}; got != [5]int{4, 3, 0, 1, 0} {
		t.Errorf("total, added, existing, unmatched, pending = %v", got)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs, thrones: syncedAs})

	show := w.item(thrones)
	if show.Type != models.WatchlistTypeShow || show.TMDbID != 1399 || show.Title != "Game of Thrones" || show.Year != 2011 {
		t.Errorf("show stored as %+v; want TMDb's type, ID, title and year", show)
	}
	if res.Outcomes[obscure] != OutcomeUnmatched || res.Outcomes[matrix] != OutcomeAdded {
		t.Errorf("outcomes = %v", res.Outcomes)
	}
	wantRefs := []models.TitleRef{{MediaType: "movie", TMDbID: 603}, {MediaType: "movie", TMDbID: 27205}, {MediaType: "tv", TMDbID: 1399}}
	if !reflect.DeepEqual(res.AddedTitles, wantRefs) {
		t.Errorf("added titles = %v, want %v", res.AddedTitles, wantRefs)
	}
	// The end of the IMDb list is the most recently added there, and stays
	// the newest here.
	listed, _ := w.store.ListWatchlistItems(context.Background(), models.ListWatchlistOptions{UserID: w.user})
	if len(listed) != 3 || listed[0].IMDbID != thrones || listed[2].IMDbID != matrix {
		t.Errorf("newest-first order does not follow the list")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn, thrones)
	writes, lookups := w.store.Writes, len(w.tmdb.lookups())

	res := w.sync(matrix, inceptn, thrones)

	if got := [5]int{res.Total, res.Added, res.Existing, res.Unmatched, res.Pending}; got != [5]int{3, 0, 3, 0, 0} {
		t.Errorf("total, added, existing, unmatched, pending = %v", got)
	}
	if w.store.Writes != writes {
		t.Errorf("a second run over the same list wrote %d times", w.store.Writes-writes)
	}
	if res.Lookups != 0 || len(w.tmdb.lookups()) != lookups {
		t.Errorf("a second run asked TMDb %d times; titles the user has need no lookup", res.Lookups)
	}
	if n := w.store.Queries["ListWatchlistItems"]; n != 2 {
		t.Errorf("the watchlist was read %d times over two runs, want once per run", n)
	}
}

// A title another user already brought in is stored; it costs no TMDb call.
func TestRunResolvesFromStoredTitlesBeforeAskingTMDb(t *testing.T) {
	w := newWorld(t)
	w.tmdb.movie(inceptn, 27205, "Inception")
	stored := &models.Title{ID: "tv:1399", MediaType: "tv", TMDbID: 1399}
	stored.TitleMetadata = models.TitleMetadata{Title: "Game of Thrones", Year: 2011, IMDbID: thrones}
	w.store.PutTitle(stored)

	res := w.sync(thrones, inceptn)

	if got := w.tmdb.lookups(); !reflect.DeepEqual(got, []string{inceptn}) || res.Lookups != 1 {
		t.Errorf("TMDb was asked about %v, want only the title that is not stored", got)
	}
	if show := w.item(thrones); show.Title != "Game of Thrones" || show.Type != models.WatchlistTypeShow || show.TMDbID != 1399 {
		t.Errorf("item from a stored title = %+v", show)
	}
	if n := w.store.Queries["GetTitlesByIMDbIDs"]; n != 1 {
		t.Errorf("stored titles were read %d times, want one query", n)
	}
}

func TestRunLeavesHandAddedItemsAlone(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	manual := w.store.AddItem(&models.WatchlistItem{
		UserID: w.user, Title: "Matrix (rewatch)", Type: models.WatchlistTypeMovie, TMDbID: 603,
		Status: models.WatchlistStatusWatching, Tags: []string{"rewatch"},
	})

	res := w.sync(matrix)

	if res.Added != 0 || res.Existing != 1 || res.Filled != 1 {
		t.Errorf("added = %d, existing = %d, filled = %d; want the hand-added item recognised", res.Added, res.Existing, res.Filled)
	}
	items := w.store.Items(w.user)
	if len(items) != 1 {
		t.Fatalf("watchlist has %d items, want the one the user added", len(items))
	}
	got := items[0]
	if got.ID != manual.ID || got.Source != "" || got.Status != models.WatchlistStatusWatching || got.Title != "Matrix (rewatch)" || !reflect.DeepEqual(got.Tags, []string{"rewatch"}) {
		t.Errorf("hand-added item was changed: %+v", got)
	}
	if got.IMDbID != matrix {
		t.Errorf("IMDb ID = %q, want it filled in", got.IMDbID)
	}

	// And it is never removed, however long it stays off the list.
	for i := 0; i < 3; i++ {
		w.now = w.now.Add(24 * time.Hour)
		w.sync(inceptn)
	}
	w.expect(map[string]string{matrix: "/watching", inceptn: syncedAs})
}

func TestRunAddsAMovieAndAShowThatShareATMDbID(t *testing.T) {
	w := newWorld(t)
	w.tmdb.movie(matrix, 550, "Fight Club").show(thrones, 550, "Some Show")

	res := w.sync(matrix, thrones)

	if res.Added != 2 {
		t.Fatalf("added = %d, want both", res.Added)
	}
	if a, b := w.item(matrix), w.item(thrones); a.Type != models.WatchlistTypeMovie || b.Type != models.WatchlistTypeShow || a.TMDbID != b.TMDbID {
		t.Errorf("items = %+v, %+v", a, b)
	}
}

func TestRunDedupesTheIncomingList(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)

	res := w.sync(matrix, matrix, inceptn, matrix)

	if res.Total != 2 || res.Added != 2 || res.Plan.Repeats != 2 {
		t.Errorf("total = %d, added = %d, repeats = %d", res.Total, res.Added, res.Plan.Repeats)
	}
	if got := w.tmdb.lookups(); !reflect.DeepEqual(got, []string{matrix, inceptn}) {
		t.Errorf("lookups = %v, want one per distinct ID", got)
	}
}

// The user adds a title by hand while the run is resolving: the insert hits
// the unique index. That is "already there", never a failure.
func TestRunTreatsADuplicateKeyOnInsertAsExisting(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.store.beforeCreate = func() {
		w.store.AddItem(&models.WatchlistItem{
			UserID: w.user, Title: "The Matrix", Type: models.WatchlistTypeMovie, TMDbID: 603,
			Status: models.WatchlistStatusFinished,
		})
	}

	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeSync, Complete: true, IDs: []string{matrix, inceptn}})

	if err != nil {
		t.Fatalf("a duplicate key failed the run: %v", err)
	}
	if got := [4]int{res.Added, res.Existing, res.Pending, res.Total}; got != [4]int{1, 1, 0, 2} {
		t.Errorf("added, existing, pending, total = %v; want the duplicate counted as existing", got)
	}
	if res.Outcomes[matrix] != OutcomeExisting || res.Outcomes[inceptn] != OutcomeAdded {
		t.Errorf("outcomes = %v", res.Outcomes)
	}
	w.expect(map[string]string{"The Matrix": "/finished", inceptn: syncedAs})
	if len(res.AddedTitles) != 1 {
		t.Errorf("added titles = %v, want only the one inserted", res.AddedTitles)
	}
}

// --- removing ---

func TestSyncRemovesOnTheSecondStrikeOnly(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn, thrones)

	// Day 2: Inception is no longer on the IMDb list.
	w.now = w.now.Add(24 * time.Hour)
	res := w.sync(matrix, thrones)
	if res.MarkedMissing != 1 || res.Removed != 0 {
		t.Errorf("strike one: marked = %d, removed = %d; want 1, 0", res.MarkedMissing, res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing", thrones: syncedAs})
	if since := w.item(inceptn).IMDbMissingSince; !since.Equal(w.now) {
		t.Errorf("missing since %v, want the time of the sync %v", since, w.now)
	}

	// The same day again, were the job to run twice: too soon.
	w.now = w.now.Add(time.Hour)
	if res := w.sync(matrix, thrones); res.Removed != 0 || res.MarkedMissing != 0 {
		t.Errorf("an hour later: marked = %d, removed = %d; want nothing", res.MarkedMissing, res.Removed)
	}

	// Day 3: still gone.
	w.now = w.now.Add(23 * time.Hour)
	res = w.sync(matrix, thrones)
	if res.Removed != 1 {
		t.Errorf("strike two: removed = %d, want 1", res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs, thrones: syncedAs})
}

func TestSyncClearsTheMarkWhenATitleReappears(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix)
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})

	w.now = w.now.Add(24 * time.Hour)
	res := w.sync(matrix, inceptn)

	if res.Unmarked != 1 || res.Removed != 0 || res.Added != 0 {
		t.Errorf("unmarked = %d, removed = %d, added = %d", res.Unmarked, res.Removed, res.Added)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs})

	// Gone again later: the count starts over.
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix); res.MarkedMissing != 1 || res.Removed != 0 {
		t.Errorf("gone again: marked = %d, removed = %d; want strike one again", res.MarkedMissing, res.Removed)
	}
}

func TestSyncNeverRemovesWhatTheUserTouched(t *testing.T) {
	edits := map[string]func(*models.WatchlistItem){
		"status changed to watching": func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusWatching },
		"status changed to finished": func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusFinished },
		"tagged":                     func(it *models.WatchlistItem) { it.Tags = []string{"with friends"} },
	}
	for name, edit := range edits {
		t.Run(name+" before the list changed", func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			w.store.EditItem(w.item(inceptn).ID, edit)

			for i := 0; i < 4; i++ {
				w.now = w.now.Add(24 * time.Hour)
				if res := w.sync(matrix); res.Removed != 0 || res.MarkedMissing != 0 {
					t.Fatalf("day %d: marked = %d, removed = %d", i+2, res.MarkedMissing, res.Removed)
				}
			}
			if len(w.store.Items(w.user)) != 2 {
				t.Errorf("watchlist = %v, want the user's item kept", w.titles())
			}
		})

		t.Run(name+" between the strikes", func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			w.now = w.now.Add(24 * time.Hour)
			w.sync(matrix) // strike one
			w.store.EditItem(w.item(inceptn).ID, edit)

			w.now = w.now.Add(24 * time.Hour)
			res := w.sync(matrix)

			if res.Removed != 0 || res.Unmarked != 1 {
				t.Errorf("removed = %d, unmarked = %d; want the item kept and its mark dropped", res.Removed, res.Unmarked)
			}
			if got := w.item(inceptn); got.IMDbMissingSince != nil {
				t.Errorf("the mark is still on an item the user owns")
			}
		})

		// The narrowest window: the plan already says "remove" when the
		// user's edit lands. The delete itself must refuse.
		t.Run(name+" between the plan and the delete", func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			w.now = w.now.Add(24 * time.Hour)
			w.sync(matrix)
			target := w.item(inceptn).ID
			w.store.beforeDelete = func() { w.store.EditItem(target, edit) }

			w.now = w.now.Add(24 * time.Hour)
			res := w.sync(matrix)

			if len(res.Plan.Remove) != 1 {
				t.Fatalf("plan removes %d items, want the test to reach the delete", len(res.Plan.Remove))
			}
			if res.Removed != 0 || len(w.store.Items(w.user)) != 2 {
				t.Errorf("removed = %d, watchlist = %v; the user's edit must win", res.Removed, w.titles())
			}
		})
	}
}

// Things that look like a list losing its titles and are not.
func TestSyncDoesNotRemoveOnAnUntrustworthyFetch(t *testing.T) {
	tests := []struct {
		name     string
		complete bool
		ids      []string
		tmdbErr  string
	}{
		{name: "partial fetch", complete: false, ids: []string{matrix}},
		{name: "partial fetch with nothing in it", complete: false, ids: nil},
		{name: "empty list", complete: true, ids: nil},
		{name: "a title that could not be looked up", complete: true, ids: []string{matrix, obscure}, tmdbErr: obscure},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn, thrones)
			w.now = w.now.Add(24 * time.Hour)
			w.sync(matrix) // strike one for Inception and Game of Thrones
			if tt.tmdbErr != "" {
				w.tmdb.errs[tt.tmdbErr] = errors.New("tmdb: status 503")
			}

			// However often the bad fetch repeats, nothing goes.
			for i := 0; i < 3; i++ {
				w.now = w.now.Add(24 * time.Hour)
				res := w.run(ModeSync, tt.complete, tt.ids...)
				if res.Removed != 0 || res.MarkedMissing != 0 {
					t.Fatalf("removed = %d, marked = %d", res.Removed, res.MarkedMissing)
				}
			}
			w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing", thrones: syncedAs + "/missing"})
		})
	}
}

func TestLinkAndCSVNeverRemove(t *testing.T) {
	for _, mode := range []Mode{ModeLink, ModeCSV} {
		t.Run(mode.String(), func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			w.now = w.now.Add(24 * time.Hour)
			w.sync(matrix) // Inception has one strike

			w.now = w.now.Add(48 * time.Hour)
			res := w.run(mode, true, thrones)

			if res.Removed != 0 || res.MarkedMissing != 0 || res.Added != 1 {
				t.Errorf("removed = %d, marked = %d, added = %d", res.Removed, res.MarkedMissing, res.Added)
			}
			if len(w.store.Items(w.user)) != 3 {
				t.Errorf("watchlist = %v, want nothing removed", w.titles())
			}
			wantSource := models.SourceIMDb
			if mode == ModeCSV {
				wantSource = models.SourceCSV
			}
			if got := w.item(thrones).Source; got != wantSource {
				t.Errorf("source of the added item = %q, want %q", got, wantSource)
			}
		})
	}
}

// Items from a list the user linked before follow the same two strikes
// against the new list, counted from the day it was linked.
func TestRelinkingStartsTheStrikesOver(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // old list: Inception has one strike

	w.now = w.now.Add(24 * time.Hour)
	w.run(ModeLink, true, thrones) // a different list is linked
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs, thrones: syncedAs})

	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(thrones); res.MarkedMissing != 2 || res.Removed != 0 {
		t.Errorf("first sync of the new list: marked = %d, removed = %d; want 2, 0", res.MarkedMissing, res.Removed)
	}
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(thrones); res.Removed != 2 {
		t.Errorf("second sync of the new list: removed = %d, want 2", res.Removed)
	}
	w.expect(map[string]string{thrones: syncedAs})
}

// --- lookups ---

func TestRunLooksUpWithBoundedConcurrency(t *testing.T) {
	w := newWorld(t)
	w.tmdb.delay = 20 * time.Millisecond
	var ids []string
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("tt%07d", 1000+i)
		ids = append(ids, id)
		w.tmdb.movie(id, 1000+i, "Title")
	}

	start := time.Now()
	res := w.sync(ids...)

	if res.Added != 40 || res.Lookups != 40 || len(w.tmdb.lookups()) != 40 {
		t.Fatalf("added = %d, lookups = %d; want 40 and 40", res.Added, res.Lookups)
	}
	if w.tmdb.maxSeen > DefaultConcurrency || w.tmdb.maxSeen < 2 {
		t.Errorf("at most %d lookups were in flight, want more than one and no more than %d", w.tmdb.maxSeen, DefaultConcurrency)
	}
	if serial := 40 * w.tmdb.delay; time.Since(start) > serial*3/4 {
		t.Errorf("40 lookups took %v; one at a time would take %v", time.Since(start), serial)
	}
	if n := w.store.Queries["CreateWatchlistItems"]; n != 1 {
		t.Errorf("items were inserted in %d batches, want 1", n)
	}
}

func TestRunKeepsGoingWhenLookupsFail(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.tmdb.errs[inceptn] = errors.New("tmdb: status 500")

	res := w.sync(matrix, inceptn, thrones, obscure)

	if got := [5]int{res.Total, res.Added, res.Existing, res.Unmatched, res.Pending}; got != [5]int{4, 2, 0, 1, 1} {
		t.Errorf("total, added, existing, unmatched, pending = %v", got)
	}
	if res.Outcomes[inceptn] != OutcomePending {
		t.Errorf("a failed lookup is %v, want pending", res.Outcomes[inceptn])
	}
	w.expect(map[string]string{matrix: syncedAs, thrones: syncedAs})

	// The next run asks again only about what is still unknown.
	delete(w.tmdb.errs, inceptn)
	w.tmdb.calls = nil
	res = w.sync(matrix, inceptn, thrones, obscure)
	if got := w.tmdb.lookups(); !reflect.DeepEqual(got, []string{inceptn, obscure}) {
		t.Errorf("second run looked up %v, want the pending and the unmatched ID", got)
	}
	if res.Added != 1 || res.Pending != 0 {
		t.Errorf("second run: added = %d, pending = %d", res.Added, res.Pending)
	}
}

// When the lookup budget runs out, what was resolved is stored and the rest
// is pending: a consistent partial result, never an error.
func TestRunAppliesWhatFitInTheLookupBudget(t *testing.T) {
	w := newWorld(t)
	w.tmdb.delay = 30 * time.Millisecond
	var ids []string
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("tt%07d", 2000+i)
		ids = append(ids, id)
		w.tmdb.movie(id, 2000+i, "Title")
	}

	res, err := w.rec.Run(context.Background(), Request{
		UserID: w.user, Mode: ModeLink, IDs: ids, Complete: true, LookupBudget: 75 * time.Millisecond,
	})

	if err != nil {
		t.Fatalf("running out of lookup time is not an error: %v", err)
	}
	if res.Added == 0 || res.Pending == 0 || res.Added+res.Pending != 64 {
		t.Fatalf("added = %d, pending = %d; want some of each, 64 together", res.Added, res.Pending)
	}
	if stored := len(w.store.Items(w.user)); stored != res.Added {
		t.Errorf("%d items stored, %d reported added", stored, res.Added)
	}
	added, pending := 0, 0
	for _, id := range ids {
		switch res.Outcomes[id] {
		case OutcomeAdded:
			added++
		case OutcomePending:
			pending++
		}
	}
	if added != res.Added || pending != res.Pending {
		t.Errorf("outcomes say %d added, %d pending; counts say %d, %d", added, pending, res.Added, res.Pending)
	}

	// Running again finishes the job without repeating what was done.
	w.tmdb.delay = 0
	w.tmdb.calls = nil
	again := w.run(ModeLink, true, ids...)
	if again.Added != res.Pending || again.Existing != res.Added || again.Lookups != res.Pending {
		t.Errorf("second run: added = %d, existing = %d, lookups = %d; want %d, %d, %d",
			again.Added, again.Existing, again.Lookups, res.Pending, res.Added, res.Pending)
	}
}

func TestRunWithoutTMDbUsesStoredTitlesOnly(t *testing.T) {
	w := newWorld(t)
	stored := &models.Title{ID: "movie:603", MediaType: "movie", TMDbID: 603}
	stored.TitleMetadata = models.TitleMetadata{Title: "The Matrix", Year: 1999, IMDbID: matrix}
	w.store.PutTitle(stored)
	rec := New(w.store, nil)
	if rec.CanResolve() {
		t.Error("CanResolve is true without a TMDb client")
	}

	res, err := rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeLink, IDs: []string{matrix, inceptn}, Complete: true})
	if err != nil || res.Added != 1 || res.Pending != 1 {
		t.Errorf("added = %d, pending = %d, err = %v", res.Added, res.Pending, err)
	}
}

// --- failures ---

func TestRunStopsBeforeWritingWhenTheWatchlistCannotBeRead(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.store.Err = errors.New("mongo: connection reset")

	_, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true})

	if err == nil {
		t.Fatal("no error although the watchlist could not be read")
	}
	if w.store.Writes != 0 || len(w.tmdb.lookups()) != 0 {
		t.Errorf("writes = %d, lookups = %d; want nothing attempted", w.store.Writes, len(w.tmdb.lookups()))
	}
}

// A run whose first write fails must not go on to delete.
func TestRunRemovesNothingAfterAFailedWrite(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // strike one for Inception
	w.now = w.now.Add(24 * time.Hour)
	w.store.createErr = errors.New("mongo: write concern timeout")

	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeSync, IDs: []string{matrix, thrones}, Complete: true})

	if err == nil {
		t.Fatal("no error although the insert failed")
	}
	if len(res.Plan.Remove) != 1 {
		t.Fatalf("plan removes %d items, want the test to have a removal to skip", len(res.Plan.Remove))
	}
	if res.Removed != 0 || res.Added != 0 || res.Pending != 1 {
		t.Errorf("removed = %d, added = %d, pending = %d; want 0, 0 and the unwritten title pending", res.Removed, res.Added, res.Pending)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})
}

func TestDryRunWritesNothing(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix)
	w.now = w.now.Add(24 * time.Hour)
	writes := w.store.Writes
	before := w.titles()

	res, err := w.rec.Run(context.Background(), Request{
		UserID: w.user, Mode: ModeSync, IDs: []string{matrix, thrones}, Complete: true, DryRun: true,
	})

	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Removed != 1 || res.Existing != 1 {
		t.Errorf("added = %d, removed = %d, existing = %d; want the plan's 1, 1, 1", res.Added, res.Removed, res.Existing)
	}
	if w.store.Writes != writes || !reflect.DeepEqual(w.titles(), before) {
		t.Errorf("a dry run wrote: %d writes, watchlist %v", w.store.Writes-writes, w.titles())
	}
}

// --- SyncList: from a fetch to the stored link state ---

func entries(ids ...string) []imdb.Entry {
	out := make([]imdb.Entry, len(ids))
	for i, id := range ids {
		out[i] = imdb.Entry{IMDbID: id, Title: "IMDb's own title", TypeID: "movie"}
	}
	return out
}

func completeList(ids ...string) imdb.List {
	return imdb.List{SourceID: "ls042518938", Entries: entries(ids...), Total: len(ids), Complete: true}
}

func TestSyncListStatus(t *testing.T) {
	tests := []struct {
		name       string
		list       imdb.List
		fetchErr   error
		tmdbErr    string
		wantStatus string
		wantError  string
		wantSynced bool
		wantItems  int
		wantTotal  int
	}{
		{
			name:       "complete list",
			list:       completeList(matrix, inceptn),
			wantStatus: models.IMDbStatusOK,
			wantSynced: true, wantItems: 2, wantTotal: 2,
		},
		{
			name:       "complete but empty list",
			list:       completeList(),
			wantStatus: models.IMDbStatusOK,
			wantSynced: true,
		},
		{
			name:       "list with entries that are not titles",
			list:       imdb.List{Entries: entries(matrix), Total: 3, Other: 2, Complete: true},
			wantStatus: models.IMDbStatusOK,
			wantSynced: true, wantItems: 1, wantTotal: 1,
		},
		{
			name:       "private or missing list",
			fetchErr:   fmt.Errorf("page 1: %w", imdb.ErrNotFound),
			wantStatus: models.IMDbStatusNotFound,
			wantError:  "IMDb list not found or not public",
		},
		{
			name:       "IMDb unreachable",
			fetchErr:   errors.New("Post \"https://api.graphql.imdb.com/\": dial tcp 10.1.2.3:443: i/o timeout"),
			wantStatus: models.IMDbStatusFailed,
			wantError:  "IMDb could not be reached",
		},
		{
			name:       "second page failed",
			list:       imdb.List{Entries: entries(matrix), Total: 300},
			fetchErr:   errors.New("imdb: status 502"),
			wantStatus: models.IMDbStatusPartial,
			wantError:  "only part of the IMDb list could be read",
			wantItems:  1, wantTotal: 300,
		},
		{
			name:       "total does not match what was read",
			list:       imdb.List{Entries: entries(matrix, inceptn), Total: 5},
			wantStatus: models.IMDbStatusPartial,
			wantError:  "only part of the IMDb list could be read",
			wantItems:  2, wantTotal: 5,
		},
		{
			name:       "a title could not be looked up",
			list:       completeList(matrix, inceptn),
			tmdbErr:    inceptn,
			wantStatus: models.IMDbStatusPartial,
			wantError:  "1 titles could not be looked up yet; the next sync tries again",
			wantSynced: true, wantItems: 1, wantTotal: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			if tt.tmdbErr != "" {
				w.tmdb.errs[tt.tmdbErr] = errors.New("tmdb: status 500")
			}

			out := w.rec.SyncList(context.Background(), SyncRequest{
				UserID: w.user, SourceID: "ls042518938", Mode: ModeSync, List: tt.list, FetchErr: tt.fetchErr,
			})

			state := out.State
			if state.LastStatus != tt.wantStatus || state.LastError != tt.wantError {
				t.Errorf("status = %q, error = %q; want %q, %q", state.LastStatus, state.LastError, tt.wantStatus, tt.wantError)
			}
			if (state.LastSyncedAt != nil) != tt.wantSynced {
				t.Errorf("last synced at = %v, want set: %v", state.LastSyncedAt, tt.wantSynced)
			}
			if state.LastAttemptAt == nil || !state.LastAttemptAt.Equal(w.now) || !state.LinkedAt.Equal(w.now) || state.SourceID != "ls042518938" {
				t.Errorf("state = %+v; want the attempt and the link stamped now", state)
			}
			if got := len(w.store.Items(w.user)); got != tt.wantItems || state.Added != tt.wantItems {
				t.Errorf("%d items stored, state says %d added; want %d", got, state.Added, tt.wantItems)
			}
			if state.Total != tt.wantTotal {
				t.Errorf("total = %d, want %d", state.Total, tt.wantTotal)
			}
		})
	}
}

// A fetch that read nothing must not reach the reconcile at all: no read of
// the watchlist, no write, and the counts of the last good run stay.
func TestSyncListDoesNothingWhenTheListCouldNotBeRead(t *testing.T) {
	for _, fetchErr := range []error{imdb.ErrNotFound, errors.New("imdb: status 503")} {
		w := newWorld(t)
		standardTMDb(w.tmdb)
		w.sync(matrix, inceptn)
		w.now = w.now.Add(24 * time.Hour)
		w.sync(matrix) // Inception has a strike and is old enough tomorrow
		w.now = w.now.Add(24 * time.Hour)
		linked, synced := w.now.Add(-72*time.Hour), w.now.Add(-24*time.Hour)
		previous := &models.IMDbSync{
			SourceID: "ls042518938", LinkedAt: linked, LastSyncedAt: &synced,
			LastStatus: models.IMDbStatusOK, Total: 2, Added: 2, Unmatched: 1,
		}
		reads, writes := w.store.Queries["ListWatchlistItems"], w.store.Writes

		out := w.rec.SyncList(context.Background(), SyncRequest{
			UserID: w.user, Previous: previous, SourceID: "ls042518938", Mode: ModeSync, FetchErr: fetchErr,
		})

		if w.store.Queries["ListWatchlistItems"] != reads || w.store.Writes != writes {
			t.Errorf("%v: the reconcile ran", fetchErr)
		}
		w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})
		state := out.State
		if !state.LinkedAt.Equal(linked) || state.LastSyncedAt == nil || !state.LastSyncedAt.Equal(synced) {
			t.Errorf("%v: linked at %v, last synced %v; want both kept", fetchErr, state.LinkedAt, state.LastSyncedAt)
		}
		if state.Total != 2 || state.Added != 2 || state.Unmatched != 1 {
			t.Errorf("%v: counts = %+v; want the last good run's", fetchErr, state)
		}
		if state.LastStatus == models.IMDbStatusOK || state.LastError == "" || !state.LastAttemptAt.Equal(w.now) {
			t.Errorf("%v: state = %+v", fetchErr, state)
		}
		if previous.LastStatus != models.IMDbStatusOK || previous.LastAttemptAt != nil {
			t.Errorf("the previous state was modified: %+v", previous)
		}
	}
}

func TestSyncListStartsANewLinkForADifferentSource(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	synced := w.now.Add(-24 * time.Hour)
	previous := &models.IMDbSync{SourceID: "ur101082417", LinkedAt: synced, LastSyncedAt: &synced, Total: 90, Added: 4, Removed: 2}

	out := w.rec.SyncList(context.Background(), SyncRequest{
		UserID: w.user, Previous: previous, SourceID: "ls042518938", Mode: ModeLink,
		FetchErr: errors.New("imdb: status 500"),
	})

	want := models.IMDbSync{
		SourceID: "ls042518938", LinkedAt: w.now, LastAttemptAt: &w.now,
		LastStatus: models.IMDbStatusFailed, LastError: "IMDb could not be reached",
	}
	if !reflect.DeepEqual(out.State, want) {
		t.Errorf("state = %+v\nwant %+v", out.State, want)
	}
}

func TestSyncListReportsAFailedWriteAsPartial(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.store.createErr = errors.New("mongodb://user:secret@cluster0.example.net: write failed")

	out := w.rec.SyncList(context.Background(), SyncRequest{
		UserID: w.user, SourceID: "ls042518938", Mode: ModeSync, List: completeList(matrix),
	})

	if out.Err == nil {
		t.Fatal("the write error was dropped")
	}
	if out.State.LastStatus != models.IMDbStatusPartial || out.State.LastSyncedAt != nil {
		t.Errorf("state = %+v; want partial and not synced", out.State)
	}
	if out.State.LastError != "the list was read but the watchlist could not be fully updated" {
		t.Errorf("last error = %q; it must never carry the database's own error text", out.State.LastError)
	}
}

// IMDb's titles are never stored: only the IDs are, the rest is TMDb's.
func TestSyncListStoresTMDbMetadataNotIMDbs(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.rec.SyncList(context.Background(), SyncRequest{
		UserID: w.user, SourceID: "ls042518938", Mode: ModeLink, List: completeList(matrix),
	})
	if got := w.item(matrix).Title; got != "The Matrix" {
		t.Errorf("title = %q, want TMDb's", got)
	}
}
