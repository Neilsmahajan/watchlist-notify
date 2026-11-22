package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

// --- fakes for the IMDb link and the CSV import ---

// fakeIMDbLists is IMDb: it serves lists by ID and counts the fetches.
type fakeIMDbLists struct {
	mu      sync.Mutex
	lists   map[string]imdb.List
	errs    map[string]error
	fetched []string
}

func (f *fakeIMDbLists) FetchList(_ context.Context, sourceID string) (imdb.List, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched = append(f.fetched, sourceID)
	list, ok := f.lists[sourceID]
	if err := f.errs[sourceID]; err != nil {
		return list, err
	}
	if !ok {
		return imdb.List{SourceID: sourceID}, imdb.ErrNotFound
	}
	return list, nil
}

func (f *fakeIMDbLists) fetches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.fetched)
}

// put makes a complete list of the given titles available under sourceID.
func (f *fakeIMDbLists) put(sourceID string, ids ...string) {
	list := imdb.List{SourceID: sourceID, Total: len(ids), Complete: true}
	for _, id := range ids {
		list.Entries = append(list.Entries, imdb.Entry{IMDbID: id, Title: "IMDb's title"})
	}
	f.lists[sourceID] = list
}

// fakeFinder is TMDb's find-by-IMDb-ID. Every "tt00NNNNN" below 9000000
// resolves to movie NNNNN unless told otherwise; the rest are unknown.
type fakeFinder struct {
	mu    sync.Mutex
	errs  map[string]error
	calls []string
	delay time.Duration
	// hang holds IDs TMDb never answers for: the lookup lasts until its
	// context ends. It makes "ran out of time" tests independent of how
	// fast the machine is.
	hang    map[string]bool
	active  int
	maxSeen int
}

func (f *fakeFinder) FindByExternalID(ctx context.Context, id, _ string) (*tmdb.Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, id)
	f.active++
	f.maxSeen = max(f.maxSeen, f.active)
	delay, err, hang := f.delay, f.errs[id], f.hang[id]
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.active--
		f.mu.Unlock()
	}()

	if hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err != nil {
		return nil, err
	}
	n := 0
	for _, c := range id[2:] {
		n = n*10 + int(c-'0')
	}
	if n >= 9000000 {
		return nil, nil
	}
	return &tmdb.Result{TMDBID: n, Title: fmt.Sprintf("Movie %d", n), Year: 2001, Type: "movie"}, nil
}

func (f *fakeFinder) lookups() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string{}, f.calls...)
	sort.Strings(out)
	return out
}

func (f *fakeFinder) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls, f.maxSeen = nil, 0
}

// syncHarness is the API around the IMDb link and the CSV import, with an
// in-memory store and fakes for IMDb and TMDb. Requests are authenticated as
// viewerEmail.
type syncHarness struct {
	t      *testing.T
	store  *storetest.Store
	user   *models.User
	lists  *fakeIMDbLists
	finder *fakeFinder
	// titles is the TMDb the availability refresher fetches baselines from.
	titles *fakeTMDb
	server *Server
	router *gin.Engine
}

func newSyncHarness(t *testing.T) *syncHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := &syncHarness{
		t:      t,
		store:  storetest.New(),
		lists:  &fakeIMDbLists{lists: map[string]imdb.List{}, errs: map[string]error{}},
		finder: &fakeFinder{errs: map[string]error{}},
		titles: &fakeTMDb{},
	}
	h.user = h.store.AddUser(&models.User{Email: viewerEmail, Region: "US"})
	h.build(h.finder)
	return h
}

// build wires the server. A nil finder is an API without a TMDb key.
func (h *syncHarness) build(finder imdbsync.Finder) {
	var fetcher availability.Fetcher
	if finder != nil {
		fetcher = h.titles
	}
	refresher := availability.NewRefresher(fetcher, h.store, nil)
	h.server = &Server{
		db:            h.store,
		refresher:     refresher,
		lister:        watchlist.NewLister(h.store, refresher, "US"),
		defaultRegion: "US",
		imdbLists:     h.lists,
		reconciler:    imdbsync.New(h.store, finder),
	}
	r := gin.New()
	api := r.Group("/api", func(c *gin.Context) { c.Set("user_email", viewerEmail) })
	api.GET("/me", h.server.meHandler)
	api.PUT("/me/imdb", h.server.linkIMDbHandler)
	api.DELETE("/me/imdb", h.server.unlinkIMDbHandler)
	api.GET("/watchlist", h.server.listWatchlistItemsHandler)
	api.POST("/watchlist/import", h.server.importWatchlistHandler)
	h.router = r
}

func (h *syncHarness) request(method, target, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *syncHarness) json(rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
	h.t.Helper()
	if rec.Code != wantStatus {
		h.t.Fatalf("status = %d, want %d; body: %s", rec.Code, wantStatus, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		h.t.Fatalf("response is not a JSON object: %v; body: %s", err, rec.Body)
	}
	return body
}

// link sends PUT /api/me/imdb.
func (h *syncHarness) link(source string, wantStatus int) map[string]any {
	h.t.Helper()
	body, _ := json.Marshal(map[string]string{"source": source})
	return h.json(h.request(http.MethodPut, "/api/me/imdb", string(body)), wantStatus)
}

// rewind moves the user's last IMDb fetch into the past, as if time passed.
func (h *syncHarness) rewind(d time.Duration) {
	user := h.store.User(h.user.ID)
	if user.IMDbAttemptAt != nil {
		at := user.IMDbAttemptAt.Add(-d)
		user.IMDbAttemptAt = &at
	}
	h.store.AddUser(user)
}

func (h *syncHarness) items() map[string]*models.WatchlistItem {
	out := map[string]*models.WatchlistItem{}
	for _, item := range h.store.Items(h.user.ID) {
		key := item.IMDbID
		if key == "" {
			key = item.Title
		}
		out[key] = item
	}
	return out
}

// upload sends a CSV to POST /api/watchlist/import.
func (h *syncHarness) upload(csv string) *httptest.ResponseRecorder {
	h.t.Helper()
	var buf bytes.Buffer
	form := multipart.NewWriter(&buf)
	part, err := form.CreateFormFile("file", "WATCHLIST.csv")
	if err != nil {
		h.t.Fatal(err)
	}
	_, _ = part.Write([]byte(csv))
	_ = form.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/watchlist/import", &buf)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func ttID(n int) string { return fmt.Sprintf("tt%07d", n) }

func ttIDs(from, count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = ttID(from + i)
	}
	return ids
}

const (
	testList      = "ls042518938"
	testWatchlist = "ur101082417"
	unknownTitle  = "tt9000001"
)

// --- PUT /api/me/imdb ---

func TestLinkIMDbAddsTheListAndStoresTheLink(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testWatchlist, ttID(603), ttID(604), unknownTitle)
	manual := h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: "Already here", Type: models.WatchlistTypeMovie, TMDbID: 604,
		Status: models.WatchlistStatusFinished, AddedAt: base, UpdatedAt: base,
	})
	before := time.Now().Add(-time.Second)

	body := h.link("https://www.imdb.com/user/ur101082417/watchlist/?ref_=nv_usr_wl_all_0", http.StatusOK)

	wantResult := map[string]any{"total": 3.0, "added": 1.0, "existing": 1.0, "unmatched": 1.0, "complete": true}
	if !reflect.DeepEqual(body["result"], wantResult) {
		t.Errorf("result = %v, want %v", body["result"], wantResult)
	}
	sync := body["imdb_sync"].(map[string]any)
	wantKeys := []string{"added", "last_attempt_at", "last_status", "last_synced_at", "linked_at", "removed", "source_id", "total", "unmatched"}
	if got := sortedKeys(sync); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("imdb_sync fields = %v, want %v", got, wantKeys)
	}
	if sync["source_id"] != testWatchlist || sync["last_status"] != "ok" || sync["total"] != 3.0 || sync["added"] != 1.0 || sync["unmatched"] != 1.0 || sync["removed"] != 0.0 {
		t.Errorf("imdb_sync = %v", sync)
	}
	for _, field := range []string{"linked_at", "last_attempt_at", "last_synced_at"} {
		at, err := time.Parse(time.RFC3339Nano, sync[field].(string))
		if err != nil || at.Before(before) || at.After(time.Now().Add(time.Second)) {
			t.Errorf("%s = %v, want now", field, sync[field])
		}
	}

	// One fetch, by the ID taken out of the URL.
	if !reflect.DeepEqual(h.lists.fetched, []string{testWatchlist}) {
		t.Errorf("fetched %v", h.lists.fetched)
	}
	// The new item carries TMDb's metadata and is marked as the sync's.
	items := h.items()
	added := items[ttID(603)]
	if added == nil || added.Source != models.SourceIMDb || added.Status != models.WatchlistStatusPlanned || added.TMDbID != 603 || added.Title != "Movie 603" {
		t.Errorf("added item = %+v", added)
	}
	// The item the user already had is theirs: untouched but for its IMDb ID.
	kept := items[ttID(604)]
	if kept == nil || kept.ID != manual.ID || kept.Source != "" || kept.Status != models.WatchlistStatusFinished || kept.Title != "Already here" {
		t.Errorf("hand-added item = %+v", kept)
	}
	if len(items) != 2 {
		t.Errorf("watchlist has %d items, want 2", len(items))
	}
	// Availability for the new title was fetched once.
	if h.titles.calls != 1 {
		t.Errorf("availability fetched %d times, want once for the one new title", h.titles.calls)
	}

	// GET /api/me now shows the link, and never the throttle field.
	me := h.json(h.request(http.MethodGet, "/api/me", ""), http.StatusOK)
	if got := me["imdb_sync"]; !reflect.DeepEqual(got, body["imdb_sync"]) {
		t.Errorf("/api/me imdb_sync = %v, want %v", got, body["imdb_sync"])
	}
	if _, leaked := me["imdb_attempt_at"]; leaked {
		t.Error("/api/me exposes imdb_attempt_at")
	}

	// GET /api/watchlist shows where each item came from.
	list := h.json(h.request(http.MethodGet, "/api/watchlist?sort=title", ""), http.StatusOK)
	sources := map[string]any{}
	for _, raw := range list["items"].([]any) {
		item := raw.(map[string]any)
		sources[item["title"].(string)] = item["source"]
	}
	if want := map[string]any{"Movie 603": "imdb", "Already here": nil}; !reflect.DeepEqual(sources, want) {
		t.Errorf("sources in the watchlist = %v, want %v", sources, want)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestLinkIMDbAcceptsIDsAndURLs(t *testing.T) {
	for source, wantID := range map[string]string{
		"ls042518938":                           testList,
		" ur101082417 ":                         testWatchlist,
		"https://www.imdb.com/list/ls042518938": testList,
		"imdb.com/user/ur101082417/watchlist":   testWatchlist,
	} {
		h := newSyncHarness(t)
		h.lists.put(wantID, ttID(1))
		body := h.link(source, http.StatusOK)
		if got := body["imdb_sync"].(map[string]any)["source_id"]; got != wantID {
			t.Errorf("source %q linked %v, want %s", source, got, wantID)
		}
	}
}

func TestLinkIMDbRejectsWhatIsNotAnIMDbList(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))

	for _, source := range []string{
		"", "tt0133093", "ls12", "my watchlist", "https://example.com/list/ls042518938",
		"https://www.imdb.com/title/tt0133093/", "ls042518938'; --", "../../ls042518938",
	} {
		body := h.link(source, http.StatusBadRequest)
		if msg, _ := body["error"].(string); !strings.Contains(msg, "ur…") || !strings.Contains(msg, "ls…") {
			t.Errorf("source %q: error = %q, want it to say what is accepted", source, msg)
		}
	}
	for _, raw := range []string{"", "not json", `{"source": 42}`, `["ls042518938"]`} {
		if rec := h.request(http.MethodPut, "/api/me/imdb", raw); rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", raw, rec.Code)
		}
	}

	if h.lists.fetches() != 0 {
		t.Errorf("invalid input reached IMDb %d times", h.lists.fetches())
	}
	if h.store.User(h.user.ID).IMDbSync != nil {
		t.Error("invalid input stored a link")
	}
	// A rejected ID is not an attempt: the corrected one goes straight through.
	h.link(testList, http.StatusOK)
}

func TestLinkIMDbPrivateOrMissingList(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.link(testList, http.StatusOK)
	linked := *h.store.User(h.user.ID).IMDbSync
	h.rewind(2 * time.Minute)

	body := h.link("ur555555555", http.StatusNotFound)

	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "Make the list public on IMDb") {
		t.Errorf("error = %q, want it to say how to fix it", msg)
	}
	if len(body) != 1 {
		t.Errorf("body = %v, want only an error", body)
	}
	// The link that worked is still there, untouched.
	if got := *h.store.User(h.user.ID).IMDbSync; !reflect.DeepEqual(got, linked) {
		t.Errorf("a failed re-link changed the stored link: %+v", got)
	}
	if len(h.items()) != 1 {
		t.Errorf("watchlist = %v", h.items())
	}
}

func TestLinkIMDbUnreachable(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.errs[testList] = errors.New("Post \"https://api.graphql.imdb.com/\": dial tcp 52.94.1.1:443: i/o timeout")

	rec := h.request(http.MethodPut, "/api/me/imdb", `{"source":"ls042518938"}`)

	body := h.json(rec, http.StatusBadGateway)
	if msg, _ := body["error"].(string); msg != "IMDb could not be reached. Try again in a few minutes." {
		t.Errorf("error = %q", msg)
	}
	if strings.Contains(rec.Body.String(), "graphql") || strings.Contains(rec.Body.String(), "52.94") {
		t.Errorf("the upstream error leaked: %s", rec.Body)
	}
	if h.store.User(h.user.ID).IMDbSync != nil {
		t.Error("an unreachable list was linked")
	}
}

func TestLinkIMDbTooLarge(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.lists.errs[testList] = fmt.Errorf("page 41: %w", imdb.ErrTooLarge)
	h.link(testList, http.StatusBadRequest)
	if h.store.User(h.user.ID).IMDbSync != nil || len(h.items()) != 0 {
		t.Error("an oversized list was linked or partly imported")
	}
}

// Every attempt that reaches IMDb counts, so that nobody can make this
// service fetch IMDb in a loop.
func TestLinkIMDbRefusesASecondAttemptWithinAMinute(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.lists.put(testWatchlist, ttID(2))
	h.link(testList, http.StatusOK)

	rec := h.request(http.MethodPut, "/api/me/imdb", `{"source":"ur101082417"}`)

	h.json(rec, http.StatusTooManyRequests)
	if got := rec.Header().Get("Retry-After"); got != "60" {
		t.Errorf("Retry-After = %q, want 60", got)
	}
	if h.lists.fetches() != 1 {
		t.Errorf("IMDb was fetched %d times, want the second attempt stopped before it", h.lists.fetches())
	}
	if got := h.store.User(h.user.ID).IMDbSync.SourceID; got != testList {
		t.Errorf("the refused attempt changed the link to %s", got)
	}

	// Unlinking does not reset it, and a failed attempt counts too.
	h.request(http.MethodDelete, "/api/me/imdb", "")
	h.link(testWatchlist, http.StatusTooManyRequests)
	h.rewind(61 * time.Second)
	h.link("ur555555555", http.StatusNotFound)
	h.link(testWatchlist, http.StatusTooManyRequests)

	h.rewind(61 * time.Second)
	h.link(testWatchlist, http.StatusOK)
	if h.lists.fetches() != 3 {
		t.Errorf("IMDb was fetched %d times in all, want 3", h.lists.fetches())
	}
}

// Two requests at the same moment: one links, the other is refused.
func TestLinkIMDbConcurrentRequestsFetchOnce(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))

	codes := make(chan int, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- h.request(http.MethodPut, "/api/me/imdb", `{"source":"ls042518938"}`).Code
		}()
	}
	wg.Wait()
	close(codes)

	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusTooManyRequests] != 5 {
		t.Errorf("status codes = %v, want one 200 and five 429", counts)
	}
	if h.lists.fetches() != 1 || len(h.items()) != 1 {
		t.Errorf("fetches = %d, items = %d; want one of each", h.lists.fetches(), len(h.items()))
	}
}

// A list too long to resolve in the time a request has: what fits is added,
// the link is stored as partial, and the next attempt carries on.
func TestLinkIMDbOverBudgetLinksAndReportsPartial(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttIDs(1000, 64)...)
	// TMDb answers for the first 16 titles and not at all for the other 48.
	h.finder.hang = map[string]bool{}
	for _, id := range ttIDs(1016, 48) {
		h.finder.hang[id] = true
	}
	h.server.resolveBudget = 300 * time.Millisecond

	body := h.link(testList, http.StatusOK)

	result := body["result"].(map[string]any)
	added := int(result["added"].(float64))
	if result["complete"] != false || result["total"] != 64.0 || added != 16 {
		t.Errorf("result = %v; want an incomplete result with 16 of the 64 added", result)
	}
	sync := body["imdb_sync"].(map[string]any)
	if sync["last_status"] != "partial" || !strings.Contains(sync["last_error"].(string), "could not be looked up yet") {
		t.Errorf("imdb_sync = %v", sync)
	}
	if len(h.items()) != added {
		t.Errorf("%d items stored, %d reported added", len(h.items()), added)
	}
	stored := h.store.User(h.user.ID).IMDbSync
	if stored == nil || stored.SourceID != testList || stored.LastStatus != models.IMDbStatusPartial || stored.Added != added {
		t.Errorf("stored link = %+v", stored)
	}

	// The next run over the same list (the worker's, a day later) only has
	// the rest to look up.
	h.finder.hang = nil
	h.finder.reset()
	h.server.resolveBudget = 0
	h.rewind(2 * time.Minute)
	again := h.link(testList, http.StatusOK)["result"].(map[string]any)
	if again["complete"] != true || int(again["added"].(float64)) != 64-added || int(again["existing"].(float64)) != added {
		t.Errorf("second result = %v, want the remaining %d added", again, 64-added)
	}
	if got := len(h.finder.lookups()); got != 64-added {
		t.Errorf("second run made %d lookups, want %d", got, 64-added)
	}
}

// IMDb failed after the first page: the titles read are added, nothing else.
func TestLinkIMDbPartlyReadListIsLinkedAsPartial(t *testing.T) {
	h := newSyncHarness(t)
	list := imdb.List{SourceID: testList, Total: 300}
	for _, id := range ttIDs(1, 3) {
		list.Entries = append(list.Entries, imdb.Entry{IMDbID: id})
	}
	h.lists.lists[testList] = list
	h.lists.errs[testList] = errors.New("imdb: status 502")

	body := h.link(testList, http.StatusOK)

	result := body["result"].(map[string]any)
	if result["complete"] != false || result["added"] != 3.0 || result["total"] != 300.0 {
		t.Errorf("result = %v", result)
	}
	sync := body["imdb_sync"].(map[string]any)
	if sync["last_status"] != "partial" || sync["last_synced_at"] != nil {
		t.Errorf("imdb_sync = %v; want partial and no last_synced_at", sync)
	}
	if len(h.items()) != 3 {
		t.Errorf("%d items stored, want the 3 that were read", len(h.items()))
	}
}

// Linking only ever adds. Items the sync added before stay, whatever state
// they are in, and so does everything from a list linked earlier.
func TestLinkIMDbNeverRemoves(t *testing.T) {
	h := newSyncHarness(t)
	longAgo := time.Now().Add(-72 * time.Hour)
	h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: "Gone from IMDb", Type: models.WatchlistTypeMovie, TMDbID: 50, IMDbID: ttID(50),
		Status: models.WatchlistStatusPlanned, Source: models.SourceIMDb, IMDbMissingSince: &longAgo, AddedAt: base, UpdatedAt: base,
	})
	h.lists.put(testWatchlist, ttID(1), ttID(2))
	h.lists.put(testList, ttID(2), ttID(3))

	h.link(testWatchlist, http.StatusOK)
	// The first link was made an hour ago.
	user := h.store.User(h.user.ID)
	firstLinkedAt := user.IMDbSync.LinkedAt.Add(-time.Hour)
	user.IMDbSync.LinkedAt = firstLinkedAt
	h.store.AddUser(user)
	h.rewind(time.Hour)
	body := h.link(testList, http.StatusOK)

	items := h.items()
	if len(items) != 4 {
		t.Fatalf("watchlist has %d items, want all four kept: %v", len(items), items)
	}
	for id, item := range items {
		if item.Source != models.SourceIMDb {
			t.Errorf("%s: source = %q", id, item.Source)
		}
		if item.IMDbMissingSince != nil {
			t.Errorf("%s still carries a missing mark from before the link", id)
		}
	}
	sync := body["imdb_sync"].(map[string]any)
	if sync["source_id"] != testList || sync["removed"] != 0.0 || sync["added"] != 1.0 {
		t.Errorf("imdb_sync after re-linking = %v", sync)
	}
	if got := h.store.User(h.user.ID).IMDbSync.LinkedAt; !got.After(firstLinkedAt) {
		t.Errorf("linked_at = %v, want a new time for a new list (was %v)", got, firstLinkedAt)
	}
}

// markedLeftover is an item the sync added and a fetch of the list linked
// before found missing three days ago.
func (h *syncHarness) markedLeftover() {
	longAgo := time.Now().Add(-72 * time.Hour)
	h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: "Gone from IMDb", Type: models.WatchlistTypeMovie, TMDbID: 50, IMDbID: ttID(50),
		Status: models.WatchlistStatusPlanned, Source: models.SourceIMDb, IMDbMissingSince: &longAgo, AddedAt: base, UpdatedAt: base,
	})
}

// The strikes of the list linked before go first. A re-link whose own writes
// then fail is still stored (the daily sync finishes it), but it must not
// sit on top of those strikes.
func TestRelinkIMDbClearsOldStrikesAlthoughItsWritesFail(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testWatchlist, ttID(1))
	h.lists.put(testList, ttID(2))
	h.link(testWatchlist, http.StatusOK)
	h.markedLeftover()
	h.rewind(time.Hour)
	h.store.Fail["CreateWatchlistItems"] = errors.New("mongo: write concern timeout")

	body := h.link(testList, http.StatusOK)

	if sync := body["imdb_sync"].(map[string]any); sync["source_id"] != testList || sync["last_status"] != models.IMDbStatusPartial {
		t.Errorf("imdb_sync = %v; want the new list linked as partial", sync)
	}
	if item := h.items()[ttID(50)]; item == nil || item.IMDbMissingSince != nil {
		t.Errorf("leftover item = %+v; want it kept and its strike from the old list cleared", item)
	}
}

// If the strikes cannot be cleared the link is not stored: the old one
// stays, nothing is added, and the user tries again.
func TestRelinkIMDbIsNotStoredWhenOldStrikesCannotBeCleared(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testWatchlist, ttID(1))
	h.lists.put(testList, ttID(2))
	h.link(testWatchlist, http.StatusOK)
	h.markedLeftover()
	h.rewind(time.Hour)
	before := h.store.User(h.user.ID).IMDbSync
	h.store.Fail["ClearIMDbMissingMarks"] = errors.New("mongo: connection reset")

	h.link(testList, http.StatusInternalServerError)

	after := h.store.User(h.user.ID).IMDbSync
	if after == nil || !reflect.DeepEqual(*after, *before) {
		t.Errorf("link = %+v, want the old one untouched: %+v", after, before)
	}
	items := h.items()
	if len(items) != 2 || items[ttID(2)] != nil {
		t.Errorf("watchlist = %v; want nothing added", items)
	}
	if item := items[ttID(50)]; item == nil || item.IMDbMissingSince == nil {
		t.Errorf("leftover item = %+v; want it as it was", item)
	}
}

func TestLinkIMDbAgainToTheSameListKeepsLinkedAt(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.link(testList, http.StatusOK)
	user := h.store.User(h.user.ID)
	first := user.IMDbSync.LinkedAt.Add(-time.Hour)
	user.IMDbSync.LinkedAt = first
	h.store.AddUser(user)
	h.rewind(time.Hour)
	h.lists.put(testList, ttID(1), ttID(2))

	body := h.link(testList, http.StatusOK)

	if got := h.store.User(h.user.ID).IMDbSync.LinkedAt; !got.Equal(first) {
		t.Errorf("linked_at moved from %v to %v", first, got)
	}
	if result := body["result"].(map[string]any); result["added"] != 1.0 || result["existing"] != 1.0 {
		t.Errorf("result = %v", result)
	}
}

func TestLinkIMDbUnavailableWithoutTMDb(t *testing.T) {
	h := newSyncHarness(t)
	h.build(nil)
	h.lists.put(testList, ttID(1))

	h.link(testList, http.StatusServiceUnavailable)

	if h.lists.fetches() != 0 || h.store.User(h.user.ID).IMDbAttemptAt != nil {
		t.Error("an API that cannot resolve titles still fetched the list or claimed the attempt")
	}
}

// --- DELETE /api/me/imdb ---

// Unlinking wipes the strikes: a sync that is running cannot remove on them
// any more, and whatever is linked next does not inherit them.
func TestUnlinkIMDbClearsEveryStrike(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.link(testList, http.StatusOK)
	h.markedLeftover()

	rec := h.request(http.MethodDelete, "/api/me/imdb", "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	items := h.items()
	if len(items) != 2 {
		t.Fatalf("watchlist = %v; want both items kept", items)
	}
	for id, item := range items {
		if item.IMDbMissingSince != nil {
			t.Errorf("%s still carries a missing mark after unlinking", id)
		}
	}
	if h.store.User(h.user.ID).IMDbSync != nil {
		t.Error("the link is still stored")
	}
}

// If the strikes cannot be wiped the answer is 500 and the list is still
// linked, so the user tries again instead of believing a half-done unlink.
func TestUnlinkIMDbKeepsTheLinkWhenStrikesCannotBeCleared(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1))
	h.link(testList, http.StatusOK)
	h.markedLeftover()
	h.store.Fail["ClearIMDbMissingMarks"] = errors.New("mongo: connection reset")

	rec := h.request(http.MethodDelete, "/api/me/imdb", "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if link := h.store.User(h.user.ID).IMDbSync; link == nil || link.SourceID != testList {
		t.Errorf("link = %+v; want it left in place", link)
	}
	if item := h.items()[ttID(50)]; item == nil || item.IMDbMissingSince == nil {
		t.Errorf("leftover item = %+v; want it as it was", item)
	}

	// The retry goes through.
	delete(h.store.Fail, "ClearIMDbMissingMarks")
	if rec := h.request(http.MethodDelete, "/api/me/imdb", ""); rec.Code != http.StatusNoContent {
		t.Errorf("retry: status = %d, want 204", rec.Code)
	}
	if h.store.User(h.user.ID).IMDbSync != nil || h.items()[ttID(50)].IMDbMissingSince != nil {
		t.Error("the retry did not unlink and clear the strike")
	}
}

func TestUnlinkIMDbKeepsTheItems(t *testing.T) {
	h := newSyncHarness(t)
	h.lists.put(testList, ttID(1), ttID(2))
	h.link(testList, http.StatusOK)

	rec := h.request(http.MethodDelete, "/api/me/imdb", "")

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q; want 204 and no body", rec.Code, rec.Body)
	}
	if h.store.User(h.user.ID).IMDbSync != nil {
		t.Error("the link is still stored")
	}
	if len(h.items()) != 2 {
		t.Errorf("unlinking removed items: %v", h.items())
	}
	me := h.json(h.request(http.MethodGet, "/api/me", ""), http.StatusOK)
	if _, linked := me["imdb_sync"]; linked {
		t.Errorf("/api/me still shows imdb_sync: %v", me["imdb_sync"])
	}

	// Unlinking again is not an error.
	if rec := h.request(http.MethodDelete, "/api/me/imdb", ""); rec.Code != http.StatusNoContent {
		t.Errorf("second unlink: status = %d", rec.Code)
	}
}
