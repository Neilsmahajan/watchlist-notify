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
	beforeAdopt  func()
	beforeMark   func()
	beforeDelete func()
	createErr    error
	clearAllErr  error
}

func (h *hookedStore) ClearIMDbMissingMarks(ctx context.Context, userID primitive.ObjectID) (int, error) {
	if h.clearAllErr != nil {
		return 0, h.clearAllErr
	}
	return h.Store.ClearIMDbMissingMarks(ctx, userID)
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

func (h *hookedStore) AdoptWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, imdbID string) (bool, error) {
	if h.beforeAdopt != nil {
		h.beforeAdopt()
	}
	return h.Store.AdoptWatchlistItem(ctx, userID, itemID, imdbID)
}

func (h *hookedStore) MarkWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID, since time.Time) (bool, error) {
	if h.beforeMark != nil {
		h.beforeMark()
	}
	return h.Store.MarkWatchlistItemIMDbMissing(ctx, userID, itemID, since)
}

func (h *hookedStore) DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince, updatedAt time.Time) (bool, error) {
	if h.beforeDelete != nil {
		h.beforeDelete()
	}
	return h.Store.DeleteSyncedWatchlistItem(ctx, userID, itemID, missingSince, updatedAt)
}

// worldList is the IMDb list the user of a world has linked.
const worldList = "ls042518938"

// world is a store, a TMDb and a clock for one user.
type world struct {
	t     *testing.T
	store *hookedStore
	tmdb  *fakeTMDb
	now   time.Time
	// linked is when the user's list was linked: the start of the world.
	linked time.Time
	user   primitive.ObjectID
	rec    *Reconciler
}

func newWorld(t *testing.T) *world {
	w := &world{
		t:     t,
		store: &hookedStore{Store: storetest.New()},
		tmdb:  newFakeTMDb(),
		now:   time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC),
	}
	w.linked = w.now
	w.user = w.store.AddUser(&models.User{
		Email:    "viewer@example.com",
		IMDbSync: &models.IMDbSync{SourceID: worldList, LinkedAt: w.linked},
	}).ID
	w.rec = New(w.store, w.tmdb, WithClock(func() time.Time { return w.now }))
	return w
}

func (w *world) run(mode Mode, complete bool, ids ...string) Result {
	w.t.Helper()
	res, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: mode, IDs: ids, Complete: complete})
	if err != nil {
		w.t.Fatalf("Run: %v", err)
	}
	return res
}

// relink stores another link for the user, the way a link request does, and
// leaves w.linked alone: runs started before it are still for the old link.
func (w *world) relink(sourceID string, at time.Time) {
	w.t.Helper()
	if err := w.store.LinkIMDb(context.Background(), w.user, models.IMDbSync{SourceID: sourceID, LinkedAt: at}); err != nil {
		w.t.Fatal(err)
	}
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

	if res.Added != 0 || res.Existing != 1 || res.Adopted != 0 {
		t.Errorf("added = %d, existing = %d, adopted = %d; want the hand-added item recognised and left the user's", res.Added, res.Existing, res.Adopted)
	}
	items := w.store.Items(w.user)
	if len(items) != 1 {
		t.Fatalf("watchlist has %d items, want the one the user added", len(items))
	}
	got := items[0]
	if got.ID != manual.ID || got.Source != "" || got.Status != models.WatchlistStatusWatching || got.Title != "Matrix (rewatch)" || !reflect.DeepEqual(got.Tags, []string{"rewatch"}) {
		t.Errorf("hand-added item was changed: %+v", got)
	}
	if got.IMDbID != "" {
		t.Errorf("IMDb ID = %q; the sync must not write one onto an item the user added by hand", got.IMDbID)
	}

	// And it is never removed, however long it stays off the list.
	for i := 0; i < 3; i++ {
		w.now = w.now.Add(24 * time.Hour)
		w.sync(inceptn)
	}
	w.expect(map[string]string{"Matrix (rewatch)": "/watching", inceptn: syncedAs})
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

	res, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, Complete: true, IDs: []string{matrix, inceptn}})

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

		// The plan already says "mark" when the user's edit lands. The mark
		// itself must refuse, so the user's item never carries a strike.
		t.Run(name+" between the plan and the mark", func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			target := w.item(inceptn).ID
			w.store.beforeMark = func() { w.store.EditItem(target, edit) }

			w.now = w.now.Add(24 * time.Hour)
			res := w.sync(matrix)

			if len(res.Plan.MarkMissing) != 1 {
				t.Fatalf("plan marks %d items, want the test to reach the mark", len(res.Plan.MarkMissing))
			}
			if res.MarkedMissing != 0 || w.item(inceptn).IMDbMissingSince != nil {
				t.Errorf("marked = %d, item = %+v; the user's edit must win", res.MarkedMissing, w.item(inceptn))
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

// An edit that leaves status and tags alone does not make the item the
// user's for good, but it still wins against a delete that was planned
// before it: the plan was drawn up for an item that has since changed.
func TestSyncDoesNotRemoveAnItemEditedAfterThePlan(t *testing.T) {
	edits := map[string]func(*models.WatchlistItem){
		"renamed":      func(it *models.WatchlistItem) { it.Title = "Inception (rewatch with Sam)" },
		"year changed": func(it *models.WatchlistItem) { it.Year = 2011 },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.sync(matrix, inceptn)
			w.now = w.now.Add(24 * time.Hour)
			w.sync(matrix) // strike one
			target := w.item(inceptn).ID
			w.store.beforeDelete = func() { w.store.EditItem(target, edit) }

			w.now = w.now.Add(24 * time.Hour)
			res := w.sync(matrix)

			if len(res.Plan.Remove) != 1 {
				t.Fatalf("plan removes %d items, want the test to reach the delete", len(res.Plan.Remove))
			}
			if res.Removed != 0 || len(w.store.Items(w.user)) != 2 {
				t.Fatalf("removed = %d, watchlist = %v; an edit made after the plan must win", res.Removed, w.titles())
			}

			// The item is still the sync's and still off the list, so a later
			// run, planning from the item as it is now, removes it.
			w.store.beforeDelete = nil
			w.now = w.now.Add(24 * time.Hour)
			if res := w.sync(matrix); res.Removed != 1 {
				t.Errorf("the next day: removed = %d, want 1", res.Removed)
			}
		})
	}
}

// The sync's own writes to an item (the mark, its clearing; adoption has its
// own test) leave updated_at alone, so they never look like a user's edit to
// the delete that follows.
func TestSyncWritesDoNotLookLikeUserEdits(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	before := w.item(inceptn).UpdatedAt

	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // mark
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix, inceptn) // clear
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // mark again

	if got := w.item(inceptn).UpdatedAt; !got.Equal(before) {
		t.Fatalf("updated_at moved from %v to %v through the sync's own writes", before, got)
	}
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix); res.Removed != 1 {
		t.Errorf("removed = %d, want the twice-missing item removed", res.Removed)
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

// --- adopting what the user imported from IMDb before linking ---

// imported puts an item on the watchlist the way an import made before the
// list was linked left it: it has the IMDb ID it was imported under, and no
// source (the oldest ones) or "csv".
func (w *world) imported(imdbID string, tmdbID int, title, source string) *models.WatchlistItem {
	w.t.Helper()
	return w.store.AddItem(&models.WatchlistItem{
		UserID: w.user, Title: title, Type: models.WatchlistTypeMovie, TMDbID: tmdbID,
		IMDbID: imdbID, Source: source, Status: models.WatchlistStatusPlanned,
	})
}

// unlinked is a world whose user has no link yet, as when PUT /api/me/imdb
// runs: the link is stored only after the reconcile.
func unlinkedWorld(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	if err := w.store.UnlinkIMDb(context.Background(), w.user); err != nil {
		t.Fatal(err)
	}
	return w
}

// The user imported their IMDb list by CSV long ago and now links that same
// list. The titles on it become the sync's, and one they later take off IMDb
// leaves here after the usual two strikes.
func TestLinkAdoptsImportedItemsAndTheSyncThenRemovesThem(t *testing.T) {
	w := unlinkedWorld(t)
	standardTMDb(w.tmdb)
	old := w.imported(matrix, 603, "The Matrix", "")
	csv := w.imported(inceptn, 27205, "Inception", models.SourceCSV)
	oldUpdated, csvUpdated := old.UpdatedAt, csv.UpdatedAt

	w.now = w.now.Add(time.Hour)
	w.linked = w.now
	res := w.run(ModeLink, true, matrix, inceptn, thrones)

	if res.Adopted != 2 || res.Added != 1 || res.Existing != 2 {
		t.Errorf("adopted = %d, added = %d, existing = %d; want 2, 1, 2", res.Adopted, res.Added, res.Existing)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs, thrones: syncedAs})
	if got := w.tmdb.lookups(); !reflect.DeepEqual(got, []string{thrones}) {
		t.Errorf("TMDb was asked about %v; items matched by their own IMDb ID need no lookup", got)
	}
	// Adoption is the sync's write, not the user's: it must not look like an
	// edit to the delete that may follow.
	if a, b := w.item(matrix).UpdatedAt, w.item(inceptn).UpdatedAt; !a.Equal(oldUpdated) || !b.Equal(csvUpdated) {
		t.Errorf("adoption moved updated_at (%v -> %v, %v -> %v)", oldUpdated, a, csvUpdated, b)
	}
	if got := w.store.Queries["GetIMDbLink"]; got != 0 {
		t.Errorf("linking read the stored link %d times; there is none to compare with yet", got)
	}
	w.relink(worldList, w.linked) // the handler stores the link after the reconcile

	// The user takes Inception off their IMDb list. Two complete fetches,
	// twelve hours apart, both without it.
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix, thrones); res.MarkedMissing != 1 || res.Removed != 0 || res.Adopted != 0 {
		t.Errorf("first fetch without it: marked = %d, removed = %d, adopted = %d; want 1, 0, 0", res.MarkedMissing, res.Removed, res.Adopted)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing", thrones: syncedAs})
	w.now = w.now.Add(MinMissingAge)
	if res := w.sync(matrix, thrones); res.Removed != 1 {
		t.Errorf("second fetch without it: removed = %d, want 1", res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs, thrones: syncedAs})
}

// A daily sync adopts as well: an imported item that turns up on the linked
// list later, or that the link request could not get to.
func TestSyncAdoptsImportedItemsOnTheList(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.imported(matrix, 603, "The Matrix", "")
	w.imported(inceptn, 27205, "Inception", models.SourceCSV)

	res := w.sync(matrix)
	if res.Adopted != 1 {
		t.Errorf("adopted = %d, want the one item that is on the list", res.Adopted)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: "csv/planned"})

	// The other one appears on the list the next day.
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix, inceptn); res.Adopted != 1 {
		t.Errorf("adopted = %d, want the item that is on the list now", res.Adopted)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs})

	// Nothing is left to adopt, and a run that only adopts read the link once.
	w.now = w.now.Add(24 * time.Hour)
	writes := w.store.Writes
	if res := w.sync(matrix, inceptn); res.Adopted != 0 || w.store.Writes != writes {
		t.Errorf("a third run adopted %d and wrote %d times; want nothing", res.Adopted, w.store.Writes-writes)
	}
	if got := w.store.Queries["GetIMDbLink"]; got != 2 {
		t.Errorf("the link was read %d times, want once for each run that adopted", got)
	}
}

// Once the user has started watching an adopted item it is theirs: it stays
// when it leaves the IMDb list, and it keeps saying where it came from.
func TestAdoptedItemTheUserStartsWatchingIsNeverRemoved(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.imported(matrix, 603, "The Matrix", "")
	w.imported(inceptn, 27205, "Inception", models.SourceCSV)
	w.run(ModeLink, true, matrix, inceptn)
	w.store.EditItem(w.item(inceptn).ID, func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusWatching })

	for day := 1; day <= 4; day++ {
		w.now = w.now.Add(24 * time.Hour)
		if res := w.sync(matrix); res.MarkedMissing != 0 || res.Removed != 0 {
			t.Fatalf("day %d: marked = %d, removed = %d; want the user's item left alone", day, res.MarkedMissing, res.Removed)
		}
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: "imdb/watching"})
}

// A title the user added by hand has no IMDb ID, which is how the sync tells
// it from an import. Being on the list must never make it the sync's: not at
// the link, not on the tenth sync, and so never removable afterwards either.
func TestHandAddedItemIsNeverAdoptedOrRemoved(t *testing.T) {
	w := unlinkedWorld(t)
	standardTMDb(w.tmdb)
	manual := w.store.AddItem(&models.WatchlistItem{
		UserID: w.user, Title: "The Matrix", Type: models.WatchlistTypeMovie, TMDbID: 603,
		Status: models.WatchlistStatusPlanned, // untouched since: only the missing IMDb ID sets it apart
	})
	check := func(when string, res Result) {
		t.Helper()
		if res.Adopted != 0 || len(res.Plan.Adopt) != 0 || res.MarkedMissing != 0 || res.Removed != 0 {
			t.Fatalf("%s: adopted = %d (planned %d), marked = %d, removed = %d; want nothing", when, res.Adopted, len(res.Plan.Adopt), res.MarkedMissing, res.Removed)
		}
		items := w.store.Items(w.user)
		var got *models.WatchlistItem
		for _, it := range items {
			if it.ID == manual.ID {
				got = it
			}
		}
		if got == nil {
			t.Fatalf("%s: the hand-added item is gone; watchlist = %v", when, w.titles())
		}
		if got.Source != "" || got.IMDbID != "" || got.IMDbMissingSince != nil || !got.UpdatedAt.Equal(manual.UpdatedAt) {
			t.Fatalf("%s: the hand-added item was written to: %+v", when, got)
		}
	}

	// Linked twice (a retry), then synced day after day with it on the list.
	check("link", w.run(ModeLink, true, matrix, inceptn))
	check("link again", w.run(ModeLink, true, matrix, inceptn))
	w.relink(worldList, w.linked)
	for day := 1; day <= 3; day++ {
		w.now = w.now.Add(24 * time.Hour)
		res := w.sync(matrix, inceptn)
		check(fmt.Sprintf("sync, day %d", day), res)
		if res.Existing != 2 || res.Added != 0 {
			t.Fatalf("day %d: existing = %d, added = %d; want the title recognised, not added again", day, res.Existing, res.Added)
		}
	}
	// It leaves the IMDb list and stays here.
	for day := 4; day <= 7; day++ {
		w.now = w.now.Add(24 * time.Hour)
		check(fmt.Sprintf("off the list, day %d", day), w.sync(inceptn))
	}
	w.expect(map[string]string{"The Matrix": "/planned", inceptn: syncedAs})
}

// A hand-added title on the list is found through the stored titles on every
// run, so leaving it without an IMDb ID costs no TMDb calls.
func TestHandAddedItemOnTheListCostsNoLookupsOnceItsTitleIsStored(t *testing.T) {
	w := newWorld(t)
	stored := &models.Title{ID: "movie:603", MediaType: "movie", TMDbID: 603}
	stored.TitleMetadata = models.TitleMetadata{Title: "The Matrix", Year: 1999, IMDbID: matrix}
	w.store.PutTitle(stored)
	w.store.AddItem(&models.WatchlistItem{
		UserID: w.user, Title: "The Matrix", Type: models.WatchlistTypeMovie, TMDbID: 603, Status: models.WatchlistStatusPlanned,
	})

	for i := 0; i < 3; i++ {
		w.now = w.now.Add(24 * time.Hour)
		if res := w.sync(matrix); res.Lookups != 0 || res.Existing != 1 || res.Added != 0 {
			t.Fatalf("run %d: lookups = %d, existing = %d, added = %d; want 0, 1, 0", i+1, res.Lookups, res.Existing, res.Added)
		}
	}
	if got := w.tmdb.lookups(); len(got) != 0 {
		t.Errorf("TMDb was asked about %v", got)
	}
}

// An import that is not on the linked list (another list's, or a title since
// taken off) is not the sync's business: never adopted, so never removed.
func TestImportedItemOffTheListIsNeverAdoptedOrRemoved(t *testing.T) {
	w := unlinkedWorld(t)
	standardTMDb(w.tmdb)
	w.imported(inceptn, 27205, "Inception", "")
	w.imported(thrones, 1399, "Game of Thrones", models.SourceCSV)

	if res := w.run(ModeLink, true, matrix); res.Adopted != 0 {
		t.Errorf("link: adopted = %d, want 0", res.Adopted)
	}
	w.relink(worldList, w.linked)
	for day := 1; day <= 4; day++ {
		w.now = w.now.Add(24 * time.Hour)
		if res := w.sync(matrix); res.Adopted != 0 || res.MarkedMissing != 0 || res.Removed != 0 {
			t.Fatalf("day %d: adopted = %d, marked = %d, removed = %d; want nothing", day, res.Adopted, res.MarkedMissing, res.Removed)
		}
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: "/planned", thrones: "csv/planned"})
}

// Importing a CSV is a one-off: it never hands anything to the sync, whether
// or not a list is linked.
func TestCSVImportNeverAdopts(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.imported(matrix, 603, "The Matrix", "")
	w.imported(inceptn, 27205, "Inception", models.SourceCSV)
	writes := w.store.Writes

	res := w.run(ModeCSV, true, matrix, inceptn)

	if res.Adopted != 0 || len(res.Plan.Adopt) != 0 || res.Existing != 2 || w.store.Writes != writes {
		t.Errorf("adopted = %d (planned %d), existing = %d, writes = %d; want 0, 0, 2, 0",
			res.Adopted, len(res.Plan.Adopt), res.Existing, w.store.Writes-writes)
	}
	w.expect(map[string]string{matrix: "/planned", inceptn: "csv/planned"})
}

// The plan says "adopt" and then the user edits the item, before the write.
// The write itself must refuse, or the sync would own an item the user has
// just made theirs and could later remove it.
func TestAdoptionLosesToAConcurrentUserEdit(t *testing.T) {
	edits := map[string]func(*models.WatchlistItem){
		"status changed to watching": func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusWatching },
		"status changed to finished": func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusFinished },
		"tagged":                     func(it *models.WatchlistItem) { it.Tags = []string{"with friends"} },
	}
	for _, mode := range []Mode{ModeLink, ModeSync} {
		for name, edit := range edits {
			t.Run(mode.String()+": "+name, func(t *testing.T) {
				w := newWorld(t)
				standardTMDb(w.tmdb)
				target := w.imported(inceptn, 27205, "Inception", models.SourceCSV).ID
				w.store.beforeAdopt = func() { w.store.EditItem(target, edit) }

				res := w.run(mode, true, inceptn)

				if len(res.Plan.Adopt) != 1 {
					t.Fatalf("plan adopts %d items, want the test to reach the write", len(res.Plan.Adopt))
				}
				if got := w.item(inceptn); res.Adopted != 0 || got.Source != models.SourceCSV {
					t.Fatalf("adopted = %d, source = %q; the user's edit must win", res.Adopted, got.Source)
				}

				// And it stays theirs for good.
				w.store.beforeAdopt = nil
				for day := 1; day <= 3; day++ {
					w.now = w.now.Add(24 * time.Hour)
					if res := w.sync(matrix); res.Adopted != 0 || res.MarkedMissing != 0 || res.Removed != 0 {
						t.Fatalf("day %d: adopted = %d, marked = %d, removed = %d", day, res.Adopted, res.MarkedMissing, res.Removed)
					}
				}
				if got := w.item(inceptn); got.Source != models.SourceCSV {
					t.Errorf("source = %q after later runs, want it still the user's", got.Source)
				}
			})
		}
	}
}

// The user deleted the item between the plan and the write: nothing to adopt,
// and nothing recreated.
func TestAdoptionOfAnItemDeletedMeanwhileDoesNothing(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	target := w.imported(inceptn, 27205, "Inception", "").ID
	w.store.beforeAdopt = func() { w.store.RemoveItem(target) }

	res := w.sync(inceptn)

	if res.Adopted != 0 || len(w.store.Items(w.user)) != 0 {
		t.Errorf("adopted = %d, watchlist = %v; want the deletion to stand", res.Adopted, w.titles())
	}
}

// Adoption makes an item removable on the strength of the linked list, so a
// worker run adopts only while the link it was started for is in force.
func TestSyncDoesNotAdoptForALinkThatIsNoLongerInForce(t *testing.T) {
	changes := map[string]func(w *world){
		"unlinked": func(w *world) {
			if err := w.store.UnlinkIMDb(context.Background(), w.user); err != nil {
				w.t.Fatal(err)
			}
		},
		"another list linked":         func(w *world) { w.relink("ur101082417", w.linked) },
		"the same list linked afresh": func(w *world) { w.relink(worldList, w.now) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			standardTMDb(w.tmdb)
			w.imported(inceptn, 27205, "Inception", models.SourceCSV)
			w.now = w.now.Add(24 * time.Hour)
			// The change lands while the run is busy adding a new title.
			w.store.beforeCreate = func() { change(w) }

			res := w.sync(inceptn, thrones)

			if !res.LinkChanged || res.Adopted != 0 || res.Added != 1 {
				t.Errorf("link changed = %v, adopted = %d, added = %d; want true, 0, 1", res.LinkChanged, res.Adopted, res.Added)
			}
			w.expect(map[string]string{inceptn: "csv/planned", thrones: syncedAs})
		})
	}
}

func TestSyncDoesNotAdoptWhenTheLinkCannotBeRead(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.imported(inceptn, 27205, "Inception", "")
	w.store.Fail["GetIMDbLink"] = errors.New("mongo: connection reset")

	res, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, IDs: []string{inceptn}, Complete: true})

	if err == nil {
		t.Fatal("no error although the link could not be read")
	}
	if res.Adopted != 0 || w.item(inceptn).Source != "" {
		t.Errorf("adopted = %d, source = %q; want nothing adopted on a guess", res.Adopted, w.item(inceptn).Source)
	}
}

// In one run, items on the list are adopted and items off it are struck: the
// two never meet on one item, and a just-adopted item is not struck by the
// same run's later writes.
func TestAdoptionAndStrikesInOneRunFallOnDifferentItems(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix)                                   // The Matrix is the sync's
	w.imported(inceptn, 27205, "Inception", "")      // imported, on the list below
	w.imported(thrones, 1399, "Game of Thrones", "") // imported, not on it

	w.now = w.now.Add(24 * time.Hour)
	res := w.sync(inceptn)

	if res.Adopted != 1 || res.MarkedMissing != 1 || res.Removed != 0 {
		t.Errorf("adopted = %d, marked = %d, removed = %d; want 1, 1, 0", res.Adopted, res.MarkedMissing, res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs + "/missing", inceptn: syncedAs, thrones: "/planned"})
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
		SourceID: worldList,
		LinkedAt: w.linked,
		UserID:   w.user, Mode: ModeLink, IDs: ids, Complete: true, LookupBudget: 75 * time.Millisecond,
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

	res, err := rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeLink, IDs: []string{matrix, inceptn}, Complete: true})
	if err != nil || res.Added != 1 || res.Pending != 1 {
		t.Errorf("added = %d, pending = %d, err = %v", res.Added, res.Pending, err)
	}
}

// --- strikes belong to one link ---

// Linking clears every mark before it does anything else, so a link whose
// own writes then fail still leaves no strike from the list linked before.
func TestLinkClearsEveryMarkBeforeAnythingElse(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // strike one for Inception
	w.now = w.now.Add(24 * time.Hour)
	w.store.createErr = errors.New("mongo: write concern timeout")

	// Linking another list, whose one new title cannot be written.
	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeLink, IDs: []string{thrones}, Complete: true, LinkedAt: w.now})

	if err == nil {
		t.Fatal("no error although the insert failed")
	}
	if res.Unmarked != 1 {
		t.Errorf("unmarked = %d, want the one strike cleared", res.Unmarked)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs})
}

// If the marks cannot be cleared, linking does nothing at all and says so,
// so that the caller does not store the link.
func TestLinkStopsWhenTheMarksCannotBeCleared(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix)
	writes := w.store.Writes
	w.tmdb.calls = nil
	w.store.clearAllErr = errors.New("mongo: connection reset")

	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeLink, IDs: []string{thrones}, Complete: true, LinkedAt: w.now})

	if !errors.Is(err, ErrMarksNotCleared) {
		t.Fatalf("err = %v, want ErrMarksNotCleared", err)
	}
	if res.Outcomes != nil || w.store.Writes != writes || len(w.tmdb.lookups()) != 0 {
		t.Errorf("outcomes = %v, writes = %d (was %d), lookups = %d; want nothing attempted", res.Outcomes, w.store.Writes, writes, len(w.tmdb.lookups()))
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})

	out := w.rec.SyncList(context.Background(), SyncRequest{UserID: w.user, SourceID: "ls042518938", Mode: ModeLink, List: completeList(thrones)})
	if !errors.Is(out.Err, ErrMarksNotCleared) {
		t.Errorf("SyncList err = %v, want ErrMarksNotCleared passed on", out.Err)
	}
}

// A strike left over from the list linked before, which is what a link whose
// clearing went wrong would leave behind, is never the first of the two. The
// item is unmarked and then has to be missing from two fetches of the list
// that is linked now.
func TestSyncNeverActsOnAStrikeFromBeforeTheLink(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix) // strike one, under the first link
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})

	// Another list is linked an hour later, and the strike survives it.
	w.linked = w.now.Add(time.Hour)
	w.relink(worldList, w.linked)

	w.now = w.now.Add(24 * time.Hour)
	res := w.sync(matrix)
	if res.Removed != 0 || res.Unmarked != 1 || res.MarkedMissing != 0 {
		t.Errorf("removed = %d, unmarked = %d, marked = %d; want the old strike cleared and nothing else", res.Removed, res.Unmarked, res.MarkedMissing)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs})

	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix); res.MarkedMissing != 1 || res.Removed != 0 {
		t.Errorf("marked = %d, removed = %d; want strike one under the new link", res.MarkedMissing, res.Removed)
	}
	w.now = w.now.Add(24 * time.Hour)
	if res := w.sync(matrix); res.Removed != 1 {
		t.Errorf("removed = %d, want strike two under the new link to remove", res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs})
}

// A worker run that does not say which link it is for removes nothing.
func TestSyncWithoutALinkTimeNeverMarksOrRemoves(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix)
	w.now = w.now.Add(24 * time.Hour)

	res, err := w.rec.Run(context.Background(), Request{UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true})

	if err != nil || res.Removed != 0 || res.MarkedMissing != 0 || res.Plan.Prune != PruneSkippedNoLink {
		t.Errorf("removed = %d, marked = %d, prune = %q, err = %v", res.Removed, res.MarkedMissing, res.Plan.Prune, err)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing"})
}

// A mark carries the time the run claimed its fetch, not the time of the
// write, so that a link made while the run was under way is newer than it.
func TestMarksCarryTheTimeOfTheRun(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	claimed := w.now.Add(-90 * time.Second)

	res, err := w.rec.Run(context.Background(), Request{
		UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true, SourceID: worldList, LinkedAt: w.linked, At: claimed,
	})

	if err != nil || res.MarkedMissing != 1 {
		t.Fatalf("marked = %d, err = %v", res.MarkedMissing, err)
	}
	if got := w.item(inceptn).IMDbMissingSince; got == nil || !got.Equal(claimed) {
		t.Errorf("missing since %v, want the run's time %v", got, claimed)
	}

	// SyncList hands the claim on, and stamps a new link with it.
	other := newWorld(t)
	standardTMDb(other.tmdb)
	out := other.rec.SyncList(context.Background(), SyncRequest{
		UserID: other.user, SourceID: "ls042518938", Mode: ModeLink, List: completeList(matrix), At: claimed,
	})
	if !out.State.LinkedAt.Equal(claimed) || out.State.LastAttemptAt == nil || !out.State.LastAttemptAt.Equal(claimed) {
		t.Errorf("linked at %v, last attempt %v; want the claim %v", out.State.LinkedAt, out.State.LastAttemptAt, claimed)
	}
}

// --- the link changes while a run is under way ---

// A run fetches and resolves for a while, and the user can unlink or link
// afresh in that time. What the run found then says nothing about their
// watchlist: it marks nothing and removes nothing. What it added stays.
func TestSyncDoesNotPruneForALinkThatIsNoLongerInForce(t *testing.T) {
	changes := map[string]func(w *world){
		"unlinked": func(w *world) {
			if err := w.store.UnlinkIMDb(context.Background(), w.user); err != nil {
				w.t.Fatal(err)
			}
		},
		// At the very moment the first one was, so that only the list differs.
		"another list linked":         func(w *world) { w.relink("ur101082417", w.linked) },
		"the same list linked afresh": func(w *world) { w.relink(worldList, w.now) },
	}
	for name, change := range changes {
		for _, strike := range []string{"strike one", "strike two"} {
			t.Run(name+" before "+strike, func(t *testing.T) {
				w := newWorld(t)
				standardTMDb(w.tmdb)
				w.sync(matrix, inceptn)
				w.now = w.now.Add(24 * time.Hour)
				want := map[string]string{matrix: syncedAs, inceptn: syncedAs, thrones: syncedAs}
				if strike == "strike two" {
					w.sync(matrix) // strike one for Inception, under the link as it was
					w.now = w.now.Add(24 * time.Hour)
					want[inceptn] = syncedAs + "/missing"
				}
				// The change lands while the run is busy adding a new title.
				w.store.beforeCreate = func() { change(w) }

				res := w.sync(matrix, thrones)

				if !res.LinkChanged || res.MarkedMissing != 0 || res.Removed != 0 || res.Added != 1 {
					t.Errorf("link changed = %v, marked = %d, removed = %d, added = %d; want true, 0, 0, 1",
						res.LinkChanged, res.MarkedMissing, res.Removed, res.Added)
				}
				w.expect(want)
			})
		}
	}
}

// The link is read again before the removals too, not only before the marks.
func TestSyncReadsTheLinkAgainBeforeRemoving(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn, thrones)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix, thrones) // strike one for Inception
	w.now = w.now.Add(24 * time.Hour)
	// Today Game of Thrones is gone too: one mark and one removal are planned,
	// and the user unlinks between the two.
	w.store.beforeMark = func() {
		if err := w.store.UnlinkIMDb(context.Background(), w.user); err != nil {
			t.Fatal(err)
		}
	}

	res := w.sync(matrix)

	if len(res.Plan.Remove) != 1 || len(res.Plan.MarkMissing) != 1 {
		t.Fatalf("plan = %d marks, %d removals; want one of each", len(res.Plan.MarkMissing), len(res.Plan.Remove))
	}
	if !res.LinkChanged || res.Removed != 0 {
		t.Errorf("link changed = %v, removed = %d; want the removal left undone", res.LinkChanged, res.Removed)
	}
	if w.item(inceptn) == nil {
		t.Error("Inception was removed after the user unlinked")
	}
}

// If the link cannot be read, nothing is marked or removed on a guess.
func TestSyncDoesNotPruneWhenTheLinkCannotBeRead(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn, thrones)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix, thrones)
	w.now = w.now.Add(24 * time.Hour)
	w.store.Fail["GetIMDbLink"] = errors.New("mongo: connection reset")

	res, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true})

	if err == nil {
		t.Fatal("no error although the link could not be read")
	}
	if res.MarkedMissing != 0 || res.Removed != 0 || res.LinkChanged {
		t.Errorf("marked = %d, removed = %d, link changed = %v; want 0, 0, false", res.MarkedMissing, res.Removed, res.LinkChanged)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs + "/missing", thrones: syncedAs})
}

// A run with nothing to mark or remove has no reason to read the link.
func TestSyncReadsTheLinkOnlyWhenItWouldPrune(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix, inceptn, thrones)
	if got := w.store.Queries["GetIMDbLink"]; got != 0 {
		t.Errorf("the link was read %d times by runs that pruned nothing", got)
	}
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix, inceptn)
	if got := w.store.Queries["GetIMDbLink"]; got != 1 {
		t.Errorf("the link was read %d times, want once before the mark", got)
	}
}

// A run that is not told which list it is for cannot match any link.
func TestSyncWithoutASourceNeverMarksOrRemoves(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)

	res, err := w.rec.Run(context.Background(), Request{LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true})

	if err != nil || res.MarkedMissing != 0 || !res.LinkChanged {
		t.Errorf("marked = %d, link changed = %v, err = %v", res.MarkedMissing, res.LinkChanged, err)
	}
}

// Unlinking wipes the user's strikes before it removes the link. A removal
// a running sync had already planned, and checked the link for, then finds
// its item unmarked and does not happen.
func TestSyncRemovalLosesToStrikesBeingWiped(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	w.sync(matrix)
	w.now = w.now.Add(24 * time.Hour)
	w.store.beforeDelete = func() {
		if _, err := w.store.ClearIMDbMissingMarks(context.Background(), w.user); err != nil {
			t.Fatal(err)
		}
	}

	res := w.sync(matrix)

	if len(res.Plan.Remove) != 1 || res.Removed != 0 {
		t.Errorf("planned %d removals, made %d; want 1 planned and none made", len(res.Plan.Remove), res.Removed)
	}
	w.expect(map[string]string{matrix: syncedAs, inceptn: syncedAs})
}

// SyncList does not call a run "synced" when its removals were left undone.
func TestSyncListReportsAChangedLinkAsPartial(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.sync(matrix, inceptn)
	w.now = w.now.Add(24 * time.Hour)
	previous := &models.IMDbSync{SourceID: worldList, LinkedAt: w.linked}
	w.relink(worldList, w.now.Add(-time.Hour))

	out := w.rec.SyncList(context.Background(), SyncRequest{
		UserID: w.user, Previous: previous, SourceID: worldList, Mode: ModeSync, List: completeList(matrix),
	})

	if !out.Result.LinkChanged || out.State.LastStatus != models.IMDbStatusPartial || out.State.LastSyncedAt != nil {
		t.Errorf("link changed = %v, state = %+v; want partial and not synced", out.Result.LinkChanged, out.State)
	}
	if out.Result.MarkedMissing != 0 {
		t.Errorf("marked = %d under a link that is no longer in force", out.Result.MarkedMissing)
	}
}

// --- failures ---

func TestRunStopsBeforeWritingWhenTheWatchlistCannotBeRead(t *testing.T) {
	w := newWorld(t)
	standardTMDb(w.tmdb)
	w.store.Err = errors.New("mongo: connection reset")

	_, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, IDs: []string{matrix}, Complete: true})

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

	res, err := w.rec.Run(context.Background(), Request{SourceID: worldList, LinkedAt: w.linked, UserID: w.user, Mode: ModeSync, IDs: []string{matrix, thrones}, Complete: true})

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
	w.imported(obscure, 77, "An Old Import", models.SourceCSV)
	writes := w.store.Writes
	before := w.titles()

	res, err := w.rec.Run(context.Background(), Request{
		SourceID: worldList,
		LinkedAt: w.linked,
		UserID:   w.user, Mode: ModeSync, IDs: []string{matrix, thrones, obscure}, Complete: true, DryRun: true,
	})

	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 1 || res.Removed != 1 || res.Existing != 2 || res.Adopted != 1 {
		t.Errorf("added = %d, removed = %d, existing = %d, adopted = %d; want the plan's 1, 1, 2, 1", res.Added, res.Removed, res.Existing, res.Adopted)
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
			// What the client reports for overlapping pages and for a list
			// that holds a title twice: never complete, so never pruned.
			name:       "a title read twice",
			list:       imdb.List{Entries: entries(matrix, inceptn), Total: 3, Repeated: 1},
			wantStatus: models.IMDbStatusPartial,
			wantError:  "a title is on the IMDb list twice, so titles taken off it are not removed here; remove the duplicate on IMDb",
			wantItems:  2, wantTotal: 2,
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
