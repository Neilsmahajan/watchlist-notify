package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestParseIMDbSyncMode(t *testing.T) {
	for value, wantDryRun := range map[string]bool{
		"": false, "apply": false, " Apply ": false,
		"dry-run": true, "DRY-RUN": true, "dryrun": true, "dry_run": true,
	} {
		if got, err := ParseIMDbSyncMode(value); err != nil || got != wantDryRun {
			t.Errorf("ParseIMDbSyncMode(%q) = %v, %v; want %v", value, got, err, wantDryRun)
		}
	}
	for _, value := range []string{"send", "true", "off", "dry"} {
		if _, err := ParseIMDbSyncMode(value); err == nil {
			t.Errorf("ParseIMDbSyncMode(%q) accepted an unknown mode", value)
		}
	}
}

// --- fakes ---

// fakeLists serves IMDb lists by source ID and records the order of fetches.
type fakeLists struct {
	lists   map[string][]string // source ID -> title IDs
	errs    map[string]error
	fetched []string
	// onFetch runs during a fetch, to change the world mid-sync.
	onFetch func(sourceID string)
	active  int
	overlap bool
}

func (f *fakeLists) FetchList(_ context.Context, sourceID string) (imdb.List, error) {
	f.active++
	if f.active > 1 {
		f.overlap = true
	}
	defer func() { f.active-- }()
	f.fetched = append(f.fetched, sourceID)
	if f.onFetch != nil {
		f.onFetch(sourceID)
	}
	if err := f.errs[sourceID]; err != nil {
		return imdb.List{SourceID: sourceID}, err
	}
	ids := f.lists[sourceID]
	list := imdb.List{SourceID: sourceID, Total: len(ids), Complete: true}
	for _, id := range ids {
		list.Entries = append(list.Entries, imdb.Entry{IMDbID: id})
	}
	return list, nil
}

// finder resolves every "tt00000NN" to movie NN.
type finder struct{}

func (finder) FindByExternalID(_ context.Context, id, _ string) (*tmdb.Result, error) {
	n := 0
	for _, c := range id[2:] {
		n = n*10 + int(c-'0')
	}
	if n >= 9000 {
		return nil, nil // unknown to TMDb
	}
	return &tmdb.Result{TMDBID: n, Title: "Title", Type: "movie"}, nil
}

type fakeBaselines struct {
	regions map[string][]string
}

func (f *fakeBaselines) Ensure(_ context.Context, refs []models.TitleRef, _ map[string]*models.Title, region string) map[string]*models.Title {
	if f.regions == nil {
		f.regions = map[string][]string{}
	}
	for _, ref := range refs {
		f.regions[region] = append(f.regions[region], ref.Key())
	}
	return nil
}

// imdbWorld is a store with linked users, a fake IMDb and a clock.
type imdbWorld struct {
	t         *testing.T
	store     *storetest.Store
	lists     *fakeLists
	baselines *fakeBaselines
	now       time.Time
	logs      bytes.Buffer
	pauses    int
	task      *IMDbTask
}

func newIMDbWorld(t *testing.T) *imdbWorld {
	w := &imdbWorld{
		t:         t,
		store:     storetest.New(),
		lists:     &fakeLists{lists: map[string][]string{}, errs: map[string]error{}},
		baselines: &fakeBaselines{},
		now:       time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC),
	}
	clock := func() time.Time { return w.now }
	w.task = &IMDbTask{
		Store:         w.store,
		Fetcher:       w.lists,
		Syncer:        imdbsync.New(w.store, finder{}, imdbsync.WithClock(clock)),
		Baselines:     w.baselines,
		DefaultRegion: "US",
		Logger:        slog.New(slog.NewJSONHandler(&w.logs, nil)),
		Now:           clock,
		Sleep:         func(context.Context, time.Duration) { w.pauses++ },
	}
	return w
}

// link adds a user linked to a list, last fetched `ago` before now.
func (w *imdbWorld) link(email, sourceID string, ago time.Duration, ids ...string) primitive.ObjectID {
	attempt := w.now.Add(-ago)
	user := w.store.AddUser(&models.User{
		Email:         email,
		Region:        "US",
		IMDbSync:      &models.IMDbSync{SourceID: sourceID, LinkedAt: attempt, LastAttemptAt: &attempt, LastStatus: models.IMDbStatusOK},
		IMDbAttemptAt: &attempt,
	})
	w.lists.lists[sourceID] = ids
	return user.ID
}

func (w *imdbWorld) run() error {
	w.t.Helper()
	w.logs.Reset()
	return w.task.Run(context.Background())
}

func (w *imdbWorld) nextDay() { w.now = w.now.Add(24 * time.Hour) }

func (w *imdbWorld) itemIDs(user primitive.ObjectID) []string {
	ids := []string{}
	for _, item := range w.store.Items(user) {
		ids = append(ids, item.IMDbID)
	}
	return ids
}

// summary returns the "imdb summary" log line.
func (w *imdbWorld) summary() string {
	w.t.Helper()
	for _, line := range strings.Split(w.logs.String(), "\n") {
		if strings.Contains(line, `"msg":"imdb summary"`) {
			return line
		}
	}
	w.t.Fatalf("no summary line in:\n%s", w.logs.String())
	return ""
}

func (w *imdbWorld) wantSummary(fields ...string) {
	w.t.Helper()
	line := w.summary()
	for _, field := range fields {
		if !strings.Contains(line, field) {
			w.t.Errorf("summary lacks %s:\n%s", field, line)
		}
	}
}

const (
	tt1 = "tt0000001"
	tt2 = "tt0000002"
	tt3 = "tt0000003"
)

// --- the task ---

func TestIMDbTaskSyncsDueUsersOneAfterAnother(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
	bob := w.link("bob@example.com", "ls0000002", 21*time.Hour, tt2, tt3, "tt0009001")
	recent := w.link("carol@example.com", "ls0000003", 19*time.Hour, tt1)

	if err := w.run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := w.itemIDs(alice); !reflect.DeepEqual(got, []string{tt1, tt2}) {
		t.Errorf("first user's watchlist = %v", got)
	}
	if got := w.itemIDs(bob); !reflect.DeepEqual(got, []string{tt2, tt3}) {
		t.Errorf("second user's watchlist = %v", got)
	}
	// Synced under 20 hours ago: not fetched, nothing changed.
	if len(w.itemIDs(recent)) != 0 || len(w.lists.fetched) != 2 {
		t.Errorf("fetched %v; a user synced 19 hours ago must be left alone", w.lists.fetched)
	}
	if w.lists.overlap {
		t.Error("two lists were fetched at the same time")
	}
	if w.pauses != 1 {
		t.Errorf("paused %d times between two fetches, want 1", w.pauses)
	}

	state := w.store.User(bob).IMDbSync
	if state.LastStatus != models.IMDbStatusOK || state.Total != 3 || state.Added != 2 || state.Unmatched != 1 || state.Removed != 0 {
		t.Errorf("stored state = %+v", state)
	}
	if !state.LastAttemptAt.Equal(w.now) || state.LastSyncedAt == nil || !state.LastSyncedAt.Equal(w.now) {
		t.Errorf("attempt at %v, synced at %v; want both now", state.LastAttemptAt, state.LastSyncedAt)
	}
	if !state.LinkedAt.Equal(w.now.Add(-21 * time.Hour)) {
		t.Errorf("linked at = %v, want it unchanged", state.LinkedAt)
	}
	w.wantSummary(`"users":2`, `"synced":2`, `"failed":0`, `"added":4`, `"removed":0`, `"marked_missing":0`, `"unmatched":1`)

	// The new titles get their availability baseline once, after the loop.
	if got := w.baselines.regions["US"]; !reflect.DeepEqual(got, []string{"movie:1", "movie:2", "movie:3"}) {
		t.Errorf("baselines requested for %v, want each added title once", got)
	}
}

// Triggering the job again the same day must not fetch any list again.
func TestIMDbTaskFetchesAListAtMostOnceADay(t *testing.T) {
	w := newIMDbWorld(t)
	w.link("alice@example.com", "ur0000001", 48*time.Hour, tt1)

	for i := 0; i < 3; i++ {
		if err := w.run(); err != nil {
			t.Fatal(err)
		}
		w.now = w.now.Add(6 * time.Hour)
	}
	if len(w.lists.fetched) != 1 {
		t.Errorf("fetched %d times within 18 hours, want once", len(w.lists.fetched))
	}

	w.now = w.now.Add(3 * time.Hour) // 21 hours after the fetch
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	if len(w.lists.fetched) != 2 {
		t.Errorf("fetched %d times, want a second fetch after 21 hours", len(w.lists.fetched))
	}
}

// Linking a list is that day's fetch: the worker waits for the next day.
func TestIMDbTaskLeavesAFreshlyLinkedUserAlone(t *testing.T) {
	w := newIMDbWorld(t)
	w.link("alice@example.com", "ur0000001", time.Hour, tt1)
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	if len(w.lists.fetched) != 0 {
		t.Errorf("fetched %v an hour after linking", w.lists.fetched)
	}
	w.wantSummary(`"users":0`)
}

// A user who was listed as due but whose list someone fetched in between
// (they re-linked a moment ago) loses the claim and is skipped.
func TestIMDbTaskSkipsAUserItCannotClaim(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	// The first fetch is the moment the second user re-links.
	bob := w.link("bob@example.com", "ls0000002", 30*time.Hour, tt2)
	w.lists.onFetch = func(string) {
		w.lists.onFetch = nil
		if ok, err := w.store.ClaimIMDbAttempt(context.Background(), bob, w.now, time.Minute); err != nil || !ok {
			t.Fatalf("test claim failed: %v, %v", ok, err)
		}
	}

	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	if len(w.lists.fetched) != 1 || len(w.itemIDs(alice)) != 1 {
		t.Errorf("fetched %v; want only the user that could be claimed", w.lists.fetched)
	}
	w.wantSummary(`"users":2`, `"synced":1`, `"skipped":1`)
}

func TestIMDbTaskOneUsersFailureDoesNotStopTheRest(t *testing.T) {
	w := newIMDbWorld(t)
	broken := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	private := w.link("bob@example.com", "ls0000002", 30*time.Hour, tt1)
	healthy := w.link("carol@example.com", "ls0000003", 30*time.Hour, tt1, tt2)
	w.lists.errs["ur0000001"] = errors.New("imdb: status 503")
	w.lists.errs["ls0000002"] = imdb.ErrNotFound

	if err := w.run(); err != nil {
		t.Fatalf("Run: %v; one healthy user is enough for the task to succeed", err)
	}

	if got := w.itemIDs(healthy); len(got) != 2 {
		t.Errorf("healthy user's watchlist = %v", got)
	}
	if s := w.store.User(broken).IMDbSync; s.LastStatus != models.IMDbStatusFailed || s.LastError == "" || !s.LastAttemptAt.Equal(w.now) {
		t.Errorf("failed user's state = %+v", s)
	}
	if s := w.store.User(private).IMDbSync; s.LastStatus != models.IMDbStatusNotFound || s.LastError != "IMDb list not found or not public" {
		t.Errorf("private list's state = %+v", s)
	}
	w.wantSummary(`"users":3`, `"synced":1`, `"failed":1`, `"not_found":1`, `"added":2`)
}

func TestIMDbTaskFailsWhenNoListCouldBeSynced(t *testing.T) {
	w := newIMDbWorld(t)
	w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	w.link("bob@example.com", "ls0000002", 30*time.Hour, tt1)
	w.lists.errs["ur0000001"] = errors.New("imdb: status 503")
	w.lists.errs["ls0000002"] = errors.New("imdb: status 503")

	if err := w.run(); err == nil {
		t.Error("Run succeeded although IMDb was down for everyone")
	}
	w.wantSummary(`"failed":2`, `"synced":0`)
}

func TestIMDbTaskSucceedsWithNobodyLinked(t *testing.T) {
	w := newIMDbWorld(t)
	w.store.AddUser(&models.User{Email: "alice@example.com"})
	if err := w.run(); err != nil {
		t.Errorf("Run: %v", err)
	}
	w.wantSummary(`"users":0`)
}

func TestIMDbTaskFailsWhenUsersCannotBeListed(t *testing.T) {
	w := newIMDbWorld(t)
	w.store.Err = errors.New("mongo: no reachable servers")
	if err := w.run(); err == nil {
		t.Error("Run succeeded although the users could not be listed")
	}
}

// A missing list must never cost the user their items, however long it
// stays missing.
func TestIMDbTaskRemovesNothingWhileAListIsUnreachable(t *testing.T) {
	for name, failure := range map[string]error{"private": imdb.ErrNotFound, "imdb down": errors.New("imdb: status 500")} {
		t.Run(name, func(t *testing.T) {
			w := newIMDbWorld(t)
			alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
			_ = w.run()
			w.lists.errs["ur0000001"] = failure
			for i := 0; i < 4; i++ {
				w.nextDay()
				_ = w.run()
			}
			if got := w.itemIDs(alice); !reflect.DeepEqual(got, []string{tt1, tt2}) {
				t.Errorf("watchlist after four days without the list = %v", got)
			}
			for _, item := range w.store.Items(alice) {
				if item.IMDbMissingSince != nil {
					t.Errorf("%s was marked missing by a fetch that read nothing", item.IMDbID)
				}
			}
		})
	}
}

// The whole path through the worker: gone from the list on two consecutive
// days removes an untouched item and keeps one the user touched.
func TestIMDbTaskTwoStrikesAcrossDailyRuns(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2, tt3)
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	for _, item := range w.store.Items(alice) {
		if item.IMDbID == tt3 {
			w.store.EditItem(item.ID, func(it *models.WatchlistItem) { it.Status = models.WatchlistStatusWatching })
		}
	}
	w.lists.lists["ur0000001"] = []string{tt1}

	w.nextDay()
	_ = w.run()
	w.wantSummary(`"marked_missing":1`, `"removed":0`)
	if got := w.itemIDs(alice); len(got) != 3 {
		t.Fatalf("after strike one the watchlist is %v", got)
	}

	w.nextDay()
	_ = w.run()
	w.wantSummary(`"marked_missing":0`, `"removed":1`)
	if got := w.itemIDs(alice); !reflect.DeepEqual(got, []string{tt1, tt3}) {
		t.Errorf("watchlist = %v; want the untouched item gone and the one being watched kept", got)
	}
	if s := w.store.User(alice).IMDbSync; s.Removed != 1 || s.Total != 1 {
		t.Errorf("state = %+v", s)
	}
}

// Unlinking while a sync is running: the items stay and the link stays gone.
func TestIMDbTaskDoesNotResurrectALinkRemovedMidSync(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	w.lists.onFetch = func(string) {
		if _, err := w.store.UnlinkIMDb(context.Background(), alice, w.now, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	if w.store.User(alice).IMDbSync != nil {
		t.Error("the sync wrote its status back over an unlink")
	}
}

// The user unlinks, links another list, or unlinks and links the same list
// again while the worker is reading their list. The run then marks nothing
// and removes nothing, keeps what it added, and does not write its status
// over the link that is there now.
func TestIMDbTaskDoesNotPruneWhenTheLinkChangesMidSync(t *testing.T) {
	changes := map[string]func(w *imdbWorld, user primitive.ObjectID) *models.IMDbSync{
		"unlinked": func(w *imdbWorld, user primitive.ObjectID) *models.IMDbSync {
			if _, err := w.store.UnlinkIMDb(context.Background(), user, w.now, 0); err != nil {
				w.t.Fatal(err)
			}
			return nil
		},
		"another list linked": func(w *imdbWorld, user primitive.ObjectID) *models.IMDbSync {
			link := models.IMDbSync{SourceID: "ls0000009", LinkedAt: w.now, LastStatus: models.IMDbStatusOK, Total: 7}
			if err := w.store.LinkIMDb(context.Background(), user, link); err != nil {
				w.t.Fatal(err)
			}
			return &link
		},
		"the same list linked afresh": func(w *imdbWorld, user primitive.ObjectID) *models.IMDbSync {
			link := models.IMDbSync{SourceID: "ur0000001", LinkedAt: w.now, LastStatus: models.IMDbStatusOK, Total: 7}
			if err := w.store.LinkIMDb(context.Background(), user, link); err != nil {
				w.t.Fatal(err)
			}
			return &link
		},
	}
	for name, change := range changes {
		for _, strike := range []string{"strike one", "strike two"} {
			t.Run(name+" before "+strike, func(t *testing.T) {
				w := newIMDbWorld(t)
				alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
				if err := w.run(); err != nil {
					t.Fatal(err)
				}
				w.lists.lists["ur0000001"] = []string{tt1}
				if strike == "strike two" {
					w.nextDay()
					_ = w.run()
					w.wantSummary(`"marked_missing":1`)
				}
				w.nextDay()
				w.lists.lists["ur0000001"] = []string{tt1, tt3}
				var linkNow *models.IMDbSync
				w.lists.onFetch = func(string) { linkNow = change(w, alice) }

				_ = w.run()

				w.wantSummary(`"marked_missing":0`, `"removed":0`, `"added":1`, `"synced":0`)
				if got := w.itemIDs(alice); !reflect.DeepEqual(got, []string{tt1, tt2, tt3}) {
					t.Errorf("watchlist = %v; want nothing removed and the new title added", got)
				}
				if !strings.Contains(w.logs.String(), "link changed during sync; nothing adopted, marked or removed") {
					t.Errorf("the log does not say why nothing was removed:\n%s", w.logs.String())
				}
				stored := w.store.User(alice).IMDbSync
				if (stored == nil) != (linkNow == nil) || (stored != nil && !reflect.DeepEqual(*stored, *linkNow)) {
					t.Errorf("link after the run = %+v; want the user's own change untouched: %+v", stored, linkNow)
				}
			})
		}
	}
}

// A mark carries the time the worker claimed the fetch, which is before the
// fetch: a link made while the list was being read is newer than the mark.
func TestIMDbTaskStampsMarksWithTheClaim(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	w.lists.lists["ur0000001"] = []string{tt1}
	w.nextDay()
	claimed := w.now
	w.lists.onFetch = func(string) { w.now = w.now.Add(40 * time.Second) } // a slow fetch

	_ = w.run()

	w.wantSummary(`"marked_missing":1`)
	for _, item := range w.store.Items(alice) {
		if item.IMDbID == tt2 && (item.IMDbMissingSince == nil || !item.IMDbMissingSince.Equal(claimed)) {
			t.Errorf("missing since %v, want the time of the claim %v", item.IMDbMissingSince, claimed)
		}
	}
	if got := w.store.User(alice).IMDbSync.LastAttemptAt; got == nil || !got.Equal(claimed) {
		t.Errorf("last attempt at %v, want the time of the claim %v", got, claimed)
	}
}

// A run may adopt, mark and remove for 45 seconds from its claim. When the
// fetch alone takes longer, what is new on the list is still added; the rest
// is left for the next sync, and the status and the logs say so.
func TestIMDbTaskOutOfTimeAddsAndLeavesTheRestForTheNextSync(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	w.lists.lists["ur0000001"] = []string{tt1, tt3} // tt2 is gone, tt3 is new
	w.nextDay()
	w.lists.onFetch = func(string) { w.now = w.now.Add(imdbsync.OwnershipLease) } // a very slow fetch

	if err := w.run(); err != nil {
		t.Fatal(err)
	}

	w.wantSummary(`"partial":1`, `"synced":0`, `"out_of_time":1`, `"added":1`, `"marked_missing":0`)
	if !strings.Contains(w.logs.String(), `"lease_expired":true`) {
		t.Errorf("the user's log line does not say the lease was over:\n%s", w.logs.String())
	}
	if got := w.itemIDs(alice); !reflect.DeepEqual(got, []string{tt1, tt2, tt3}) {
		t.Errorf("watchlist = %v; want the new title added", got)
	}
	for _, item := range w.store.Items(alice) {
		if item.IMDbMissingSince != nil {
			t.Errorf("%s was marked after the lease was over", item.IMDbID)
		}
	}
	state := w.store.User(alice).IMDbSync
	if state.LastStatus != models.IMDbStatusPartial || state.LastError != "the sync ran out of time; it continues at the next sync" {
		t.Errorf("status = %q, last error = %q", state.LastStatus, state.LastError)
	}

	// The next day's fetch is quick, and the run does what was left.
	w.lists.onFetch = nil
	w.nextDay()
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	w.wantSummary(`"synced":1`, `"out_of_time":0`, `"marked_missing":1`)
	if state := w.store.User(alice).IMDbSync; state.LastStatus != models.IMDbStatusOK || state.LastError != "" {
		t.Errorf("next day: status = %q, last error = %q", state.LastStatus, state.LastError)
	}
}

// The pause between two users comes before the second user's claim. The
// claim starts that user's 45 seconds; a pause after it would spend them.
func TestIMDbTaskPausesBeforeItClaims(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	bob := w.link("bob@example.com", "ur0000002", 25*time.Hour, tt2)
	started := w.now
	w.task.Sleep = func(_ context.Context, d time.Duration) { w.now = w.now.Add(d) }

	if err := w.run(); err != nil {
		t.Fatal(err)
	}

	if got := w.store.User(alice).IMDbAttemptAt; got == nil || !got.Equal(started) {
		t.Errorf("the first user was claimed at %v, want %v: nothing to wait for", got, started)
	}
	if got, want := w.store.User(bob).IMDbAttemptAt, started.Add(imdbPause); got == nil || !got.Equal(want) {
		t.Errorf("the second user was claimed at %v, want %v, after the pause", got, want)
	}
}

// The job's tasks share one Cloud Run task timeout. The release script sets
// it, and it has to leave the refresh and the digest their time even when
// the IMDb sync takes as long as its budgets allow.
func TestJobTaskTimeoutCoversTheIMDbTask(t *testing.T) {
	if imdbWorstCase != 422*time.Second {
		t.Errorf("worst case = %v; the sums in the comment next to the budgets say 422 s", imdbWorstCase)
	}
	if left := JobTaskTimeout - imdbWorstCase; left < jobTimeLeftForOthers {
		t.Errorf("a worst-case IMDb sync leaves the refresh and the digest %v of %v, want at least %v",
			left, JobTaskTimeout, jobTimeLeftForOthers)
	}

	script, err := os.ReadFile("../../scripts/release-worker.sh")
	if err != nil {
		t.Fatal(err)
	}
	variable := regexp.MustCompile(`(?m)^TASK_TIMEOUT="(\d+)s"$`).FindSubmatch(script)
	if variable == nil {
		t.Fatal(`scripts/release-worker.sh no longer sets TASK_TIMEOUT="<seconds>s"`)
	}
	deployed, err := time.ParseDuration(string(variable[1]) + "s")
	if err != nil {
		t.Fatal(err)
	}
	if deployed != JobTaskTimeout {
		t.Errorf("the release script deploys with a task timeout of %v, the budgets assume %v", deployed, JobTaskTimeout)
	}
	// Both the create and the update of the job must use it, and nothing else.
	uses := regexp.MustCompile(`--task-timeout\s+(\S+)`).FindAllSubmatch(script, -1)
	if len(uses) != 2 {
		t.Fatalf("the release script sets --task-timeout %d times, want on create and on update", len(uses))
	}
	for _, use := range uses {
		if got := strings.Trim(string(use[1]), `\)`); got != `"${TASK_TIMEOUT}"` {
			t.Errorf("--task-timeout %s; want \"${TASK_TIMEOUT}\"", got)
		}
	}
}

func TestIMDbTaskDryRunWritesNothing(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2)
	before := *w.store.User(alice).IMDbSync
	writes := w.store.Writes
	w.task.DryRun = true

	if err := w.run(); err != nil {
		t.Fatal(err)
	}

	if w.store.Writes != writes {
		t.Errorf("a dry run made %d writes", w.store.Writes-writes)
	}
	user := w.store.User(alice)
	if !reflect.DeepEqual(*user.IMDbSync, before) || !user.IMDbAttemptAt.Equal(w.now.Add(-30*time.Hour)) {
		t.Errorf("a dry run changed the user's link: %+v", user.IMDbSync)
	}
	if len(w.store.Items(alice)) != 0 || len(w.baselines.regions) != 0 {
		t.Error("a dry run added items or fetched baselines")
	}
	// It still says what it would do.
	w.wantSummary(`"dry_run":true`, `"users":1`, `"added":2`)
	if !strings.Contains(w.logs.String(), `"msg":"imdb: user synced"`) || !strings.Contains(w.logs.String(), alice.Hex()) {
		t.Errorf("the dry run did not log the user's plan:\n%s", w.logs.String())
	}
}

func TestIMDbTaskLogsNoEmailAddresses(t *testing.T) {
	w := newIMDbWorld(t)
	w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	w.link("bob@example.com", "ls0000002", 30*time.Hour, tt1)
	w.lists.errs["ls0000002"] = errors.New("imdb: status 503")
	_ = w.run()

	logs := w.logs.String()
	if strings.Contains(logs, "@") || strings.Contains(logs, "alice") || strings.Contains(logs, "bob") {
		t.Errorf("the log names a user:\n%s", logs)
	}
	if !strings.Contains(logs, `"user_id"`) {
		t.Errorf("the log does not identify users by ID:\n%s", logs)
	}
}

func TestIMDbTaskStopsStartingUsersAfterCancellation(t *testing.T) {
	w := newIMDbWorld(t)
	w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1)
	w.link("bob@example.com", "ls0000002", 30*time.Hour, tt1)
	ctx, cancel := context.WithCancel(context.Background())
	w.lists.onFetch = func(string) { cancel() }

	_ = w.task.Run(ctx)

	if len(w.lists.fetched) != 1 {
		t.Errorf("fetched %v after cancellation, want the run to stop at the first user", w.lists.fetched)
	}
	w.wantSummary(`"skipped":1`)
}

// A run that is out of time leaves the remaining users for the next run,
// without using up their fetch of the day, and the next run starts with them.
func TestIMDbTaskDefersUsersWhenOutOfTime(t *testing.T) {
	w := newIMDbWorld(t)
	longest := w.link("alice@example.com", "ur0000001", 50*time.Hour, tt1)
	w.link("bob@example.com", "ls0000002", 40*time.Hour, tt1)
	last := w.link("carol@example.com", "ls0000003", 30*time.Hour, tt1)
	w.task.Budget = time.Minute
	w.lists.onFetch = func(string) { w.now = w.now.Add(40 * time.Second) } // every sync takes 40 s

	if err := w.run(); err != nil {
		t.Fatal(err)
	}

	if want := []string{"ur0000001", "ls0000002"}; !reflect.DeepEqual(w.lists.fetched, want) {
		t.Errorf("fetched %v, want %v: longest wait first, then stop when the minute is up", w.lists.fetched, want)
	}
	w.wantSummary(`"users":3`, `"synced":2`, `"deferred":1`)
	if got := w.itemIDs(longest); len(got) != 1 {
		t.Errorf("the first user's watchlist = %v", got)
	}
	if got := w.itemIDs(last); len(got) != 0 {
		t.Errorf("the deferred user's watchlist = %v, want it untouched", got)
	}

	// The deferred user's fetch of the day was not used up: a run moments
	// later syncs exactly them.
	w.lists.fetched = nil
	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ls0000003"}; !reflect.DeepEqual(w.lists.fetched, want) {
		t.Errorf("the next run fetched %v, want %v", w.lists.fetched, want)
	}
	w.wantSummary(`"users":1`, `"synced":1`, `"deferred":0`)
}

// A user who imported their list by CSV before linking it: the daily run
// takes over the imports that are on the list, says so in its logs, and
// later removes one that leaves the list. A title added by hand is never
// taken over and so never removed.
func TestIMDbTaskAdoptsImportedItemsAndLaterRemovesThem(t *testing.T) {
	w := newIMDbWorld(t)
	alice := w.link("alice@example.com", "ur0000001", 30*time.Hour, tt1, tt2, tt3)
	w.store.AddItem(&models.WatchlistItem{
		UserID: alice, Title: "Imported", Type: models.WatchlistTypeMovie, TMDbID: 1, IMDbID: tt1,
		Source: models.SourceCSV, Status: models.WatchlistStatusPlanned,
	})
	manual := w.store.AddItem(&models.WatchlistItem{
		UserID: alice, Title: "Added by hand", Type: models.WatchlistTypeMovie, TMDbID: 2, Status: models.WatchlistStatusPlanned,
	})
	sources := func() map[string]string {
		out := map[string]string{}
		for _, item := range w.store.Items(alice) {
			out[item.Title] = item.Source
		}
		return out
	}

	if err := w.run(); err != nil {
		t.Fatal(err)
	}
	w.wantSummary(`"adopted":1`, `"added":1`, `"marked_missing":0`, `"removed":0`)
	if !strings.Contains(w.logs.String(), `"msg":"imdb: user synced"`) || strings.Count(w.logs.String(), `"adopted":1`) != 2 {
		t.Errorf("the user's log line and the summary should both say adopted 1:\n%s", w.logs.String())
	}
	if got, want := sources(), (map[string]string{"Imported": models.SourceIMDb, "Added by hand": "", "Title": models.SourceIMDb}); !reflect.DeepEqual(got, want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}

	// Both leave the IMDb list.
	w.lists.lists["ur0000001"] = []string{tt3}
	w.nextDay()
	_ = w.run()
	w.wantSummary(`"adopted":0`, `"marked_missing":1`, `"removed":0`)
	w.nextDay()
	_ = w.run()
	w.wantSummary(`"adopted":0`, `"marked_missing":0`, `"removed":1`)
	if got, want := sources(), (map[string]string{"Added by hand": "", "Title": models.SourceIMDb}); !reflect.DeepEqual(got, want) {
		t.Errorf("sources = %v, want the adopted import gone and the hand-added title kept", got)
	}
	for _, item := range w.store.Items(alice) {
		if item.ID == manual.ID && (item.IMDbID != "" || item.IMDbMissingSince != nil) {
			t.Errorf("the hand-added item was written to: %+v", item)
		}
	}
}
