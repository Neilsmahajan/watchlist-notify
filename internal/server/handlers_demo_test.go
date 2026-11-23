package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	gingzip "github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

// demoHarness is the API with the public demo routes, an in-memory store and
// a fake TMDb that must never be called.
type demoHarness struct {
	t      *testing.T
	store  *storetest.Store
	tmdb   *fakeTMDb
	server *Server
	router *gin.Engine
	demo   *models.User
	now    time.Time
}

// newDemoHarness wires the demo routes the way RegisterRoutes does. authEmail
// is the address the protected routes are authenticated as.
func newDemoHarness(t *testing.T, authEmail string) *demoHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	store := storetest.New()
	fake := &fakeTMDb{}
	refresher := availability.NewRefresher(fake, store, nil)
	s := &Server{
		db:            store,
		refresher:     refresher,
		lister:        watchlist.NewLister(store, refresher, "US"),
		defaultRegion: "US",
	}
	h := &demoHarness{t: t, store: store, tmdb: fake, server: s, now: base}
	s.demoCache().now = func() time.Time { return h.now }

	r := gin.New()
	r.Use(gingzip.Gzip(gingzip.DefaultCompression, gingzip.WithExcludedPaths([]string{"/swagger"})))
	r.GET("/api/demo/watchlist", s.demoWatchlistHandler)
	r.GET("/api/demo/services", s.demoServicesHandler)
	api := r.Group("/api", func(c *gin.Context) { c.Set("user_email", authEmail) })
	api.GET("/me", s.meHandler)
	api.GET("/watchlist", s.listWatchlistItemsHandler)
	api.POST("/watchlist", s.createWatchlistItemHandler)
	h.router = r
	return h
}

// seed creates the demo account with three stored titles, and another user
// whose list must never show up in the demo.
func (h *demoHarness) seed() {
	h.demo = h.store.AddUser(&models.User{
		Email:  DemoUserEmail,
		Name:   "Demo",
		Region: "US",
		Services: []models.ServiceSubscription{
			{Code: models.ServiceNetflix, Active: true},
			{Code: models.ServiceHulu, Active: true},
		},
	})
	h.addItem(h.demo, "The Matrix", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 603, 1)
	h.addItem(h.demo, "Game of Thrones", models.WatchlistTypeShow, models.WatchlistStatusWatching, 1399, 2)
	h.addItem(h.demo, "Arrival", models.WatchlistTypeMovie, models.WatchlistStatusFinished, 329865, 3)
	h.putTitle("movie:603", offer(8, "Netflix", models.AccessSubscription), offer(10, "Amazon Video", models.AccessRent))
	h.putTitle("tv:1399", offer(1899, "HBO Max", models.AccessSubscription))
	h.putTitle("movie:329865", offer(15, "Hulu", models.AccessAds))

	other := h.store.AddUser(&models.User{Email: "someone@example.com", Region: "US"})
	h.addItem(other, "Private Title", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 550, 1)
	h.putTitle("movie:550", offer(8, "Netflix", models.AccessSubscription))
}

func (h *demoHarness) addItem(user *models.User, name, typ, status string, tmdbID, daysAgo int) {
	h.store.AddItem(&models.WatchlistItem{
		UserID: user.ID, Title: name, Type: typ, Status: status, Year: 2000 + daysAgo, TMDbID: tmdbID,
		AddedAt: base.AddDate(0, 0, -daysAgo), UpdatedAt: base,
	})
}

func (h *demoHarness) putTitle(key string, offers ...models.ProviderOffer) {
	h.store.PutTitle(&models.Title{
		ID:            key,
		TitleMetadata: models.TitleMetadata{PosterPath: "/poster.jpg", Rating: 7.5, Runtime: 100, Genres: []string{"Drama"}},
		Providers: map[string]models.RegionSnapshot{
			"US": {Link: "https://example.test/" + key, Offers: offers, FetchedAt: base},
		},
	})
}

func (h *demoHarness) do(method, target string, header http.Header) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for key, values := range header {
		req.Header[key] = values
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

func (h *demoHarness) get(target string, wantStatus int) map[string]any {
	h.t.Helper()
	rec := h.do(http.MethodGet, target, nil)
	if rec.Code != wantStatus {
		h.t.Fatalf("GET %s: status = %d, want %d; body: %s", target, rec.Code, wantStatus, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		h.t.Fatalf("GET %s: response is not a JSON object: %v; body: %s", target, err, rec.Body)
	}
	return body
}

// reads is how many times the store has been queried.
func (h *demoHarness) reads() int {
	total := 0
	for _, n := range h.store.Queries {
		total += n
	}
	return total
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestDemoWatchlistHasTheShapeOfTheRealList(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()
	// An ordinary account, read through the authenticated route, to compare
	// the response shape with.
	h.store.AddUser(&models.User{Email: viewerEmail, Region: "US", Services: h.demo.Services})

	rec := h.do(http.MethodGet, "/api/demo/watchlist", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
	var demo map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &demo); err != nil {
		t.Fatal(err)
	}
	real := h.get("/api/watchlist", http.StatusOK)

	if got, want := keysOf(demo), keysOf(real); !reflect.DeepEqual(got, want) {
		t.Errorf("top-level keys = %v, want %v", got, want)
	}
	demoMeta, realMeta := demo["meta"].(map[string]any), real["meta"].(map[string]any)
	if got, want := keysOf(demoMeta), keysOf(realMeta); !reflect.DeepEqual(got, want) {
		t.Errorf("meta keys = %v, want %v", got, want)
	}

	if got, want := itemTitles(demo), []string{"The Matrix", "Game of Thrones", "Arrival"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("titles = %v, want %v (newest first, and nobody else's)", got, want)
	}
	matrix := demo["items"].([]any)[0].(map[string]any)
	if matrix["poster_path"] != "/poster.jpg" || matrix["rating"] != 7.5 {
		t.Errorf("title metadata missing: %v", matrix)
	}
	avail := matrix["availability"].(map[string]any)
	providers := avail["providers"].([]any)
	if len(providers) != 1 || providers[0].(map[string]any)["code"] != models.ServiceNetflix {
		t.Errorf("providers = %v, want Netflix", providers)
	}
	if elsewhere := avail["elsewhere"].([]any); len(elsewhere) != 1 {
		t.Errorf("elsewhere = %v, want the rental", elsewhere)
	}
	facets := demoMeta["facets"].(map[string]any)
	if facets["available"] != float64(2) || facets["unavailable"] != float64(1) {
		t.Errorf("facets = %v", facets)
	}
	if demoMeta["total"] != float64(3) || demoMeta["region"] != "US" {
		t.Errorf("meta = %v", demoMeta)
	}
}

func TestDemoWatchlistFiltersSortsAndPages(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	cases := []struct {
		query string
		want  []string
	}{
		{"status=watching", []string{"Game of Thrones"}},
		{"type=movie", []string{"The Matrix", "Arrival"}},
		{"search=THRONES", []string{"Game of Thrones"}},
		{"service=max", []string{"Game of Thrones"}},
		{"service=netflix,hulu", []string{"The Matrix", "Arrival"}},
		{"available=true", []string{"The Matrix", "Arrival"}},
		{"available=false", []string{"Game of Thrones"}},
		{"sort=title", []string{"Arrival", "Game of Thrones", "The Matrix"}},
		{"limit=1&offset=1", []string{"Game of Thrones"}},
	}
	for _, tc := range cases {
		body := h.get("/api/demo/watchlist?"+tc.query, http.StatusOK)
		if got := itemTitles(body); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: titles = %v, want %v", tc.query, got, tc.want)
		}
	}

	paged := h.get("/api/demo/watchlist?limit=2", http.StatusOK)["meta"].(map[string]any)
	if paged["next_offset"] != float64(2) || paged["total"] != float64(3) || paged["returned"] != float64(2) {
		t.Errorf("paging meta = %v", paged)
	}
}

func TestDemoWatchlistLimits(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	for query, want := range map[string]float64{
		"":           20,
		"limit=0":    60, // "everything" on the real list; the cap here
		"limit=1000": 60,
		"limit=60":   60,
		"limit=7":    7,
	} {
		meta := h.get("/api/demo/watchlist?"+query, http.StatusOK)["meta"].(map[string]any)
		if meta["limit"] != want {
			t.Errorf("%q: limit = %v, want %v", query, meta["limit"], want)
		}
	}

	longest := strings.Repeat("é", 64) // 64 characters, 128 bytes
	h.get("/api/demo/watchlist?search="+longest, http.StatusOK)

	for _, query := range []string{
		"search=" + longest + "x",
		"limit=-1", "limit=abc", "offset=-1", "offset=abc",
		"status=nope", "type=nope", "service=nope", "available=maybe",
	} {
		body := h.get("/api/demo/watchlist?"+query, http.StatusBadRequest)
		if body["error"] == "" {
			t.Errorf("%s: no error message", query)
		}
	}
}

func TestDemoIsUnavailableUntilSeeded(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)

	for _, target := range []string{"/api/demo/watchlist", "/api/demo/services"} {
		rec := h.do(http.MethodGet, target, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, rec.Code)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"demo not available"}` {
			t.Errorf("%s: body = %s", target, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store", target, got)
		}
	}

	// A missing account is not looked up again on every request…
	before := h.reads()
	h.get("/api/demo/watchlist", http.StatusNotFound)
	if got := h.reads(); got != before {
		t.Errorf("a repeated miss queried the store %d times", got-before)
	}
	// …but the demo appears soon after the seeder has run.
	h.seed()
	h.now = h.now.Add(demoRetryAfter)
	h.get("/api/demo/watchlist", http.StatusOK)
}

func TestDemoNeverCallsTMDb(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()
	// A title nothing is stored for: the real list would fetch it.
	h.addItem(h.demo, "Unfetched", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 27205, 0)
	h.tmdb.streamsOn("movie", 27205)

	body := h.get("/api/demo/watchlist?search=unfetched", http.StatusOK)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	if avail := items[0].(map[string]any)["availability"]; avail != nil {
		t.Errorf("availability = %v, want null", avail)
	}
	if h.tmdb.calls != 0 {
		t.Errorf("TMDb was called %d times by an anonymous request", h.tmdb.calls)
	}
	if h.store.Writes != 0 {
		t.Errorf("the demo wrote to the store %d times", h.store.Writes)
	}
}

func TestDemoReadsTheDatabaseOncePerInterval(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	h.get("/api/demo/watchlist", http.StatusOK)
	first := h.reads()
	if first != 3 {
		t.Errorf("first request made %d queries, want 3 (user, items, titles)", first)
	}

	// Whatever is asked next is answered from memory.
	for _, query := range []string{"", "search=a", "search=ab", "service=netflix", "sort=-rating&limit=1", "offset=2"} {
		h.get("/api/demo/watchlist?"+query, http.StatusOK)
	}
	h.get("/api/demo/services", http.StatusOK)
	if got := h.reads(); got != first {
		t.Errorf("%d more queries within the interval, want none", got-first)
	}

	// A title added meanwhile shows up after the interval.
	h.addItem(h.demo, "Added Later", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 27205, 0)
	h.now = h.now.Add(demoCacheTTL - time.Second)
	if total := h.get("/api/demo/watchlist", http.StatusOK)["meta"].(map[string]any)["total"]; total != float64(3) {
		t.Errorf("total = %v before the interval ended, want 3", total)
	}
	h.now = h.now.Add(time.Second)
	if total := h.get("/api/demo/watchlist", http.StatusOK)["meta"].(map[string]any)["total"]; total != float64(4) {
		t.Errorf("total = %v after the interval, want 4", total)
	}
	if got := h.reads(); got != 2*first {
		t.Errorf("reload made %d queries, want %d", got-first, first)
	}
}

func TestDemoKeepsServingThroughADatabaseOutage(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()
	h.get("/api/demo/watchlist", http.StatusOK)

	h.store.Err = errors.New("database is down")
	h.now = h.now.Add(demoCacheTTL)
	if got := itemTitles(h.get("/api/demo/watchlist", http.StatusOK)); len(got) != 3 {
		t.Errorf("titles during the outage = %v, want the previous copy", got)
	}

	// The failed reload is not repeated on every request.
	before := h.reads()
	h.get("/api/demo/watchlist", http.StatusOK)
	if got := h.reads(); got != before {
		t.Errorf("%d queries right after a failed reload, want none", got-before)
	}
}

func TestDemoWithoutADatabaseAnswers500(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.store.Err = errors.New("database is down")

	rec := h.do(http.MethodGet, "/api/demo/watchlist", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "database is down") {
		t.Errorf("the response leaks the database error: %s", rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestDemoIsReadOnly(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range []string{"/api/demo/watchlist", "/api/demo/services", "/api/demo/watchlist/abc"} {
			if rec := h.do(method, target, nil); rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status = %d, want 404 or 405", method, target, rec.Code)
			}
		}
	}
	if h.store.Writes != 0 {
		t.Errorf("store writes = %d, want 0", h.store.Writes)
	}
}

func TestDemoServices(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	rec := h.do(http.MethodGet, "/api/demo/services", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", got)
	}
	var body DemoServicesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Services) != len(models.ServiceCatalog) {
		t.Fatalf("%d services, want the whole catalog (%d)", len(body.Services), len(models.ServiceCatalog))
	}
	var active []string
	for _, svc := range body.Services {
		if svc.Name == "" || svc.Access == "" {
			t.Errorf("service %q has no name or access", svc.Code)
		}
		if svc.Active {
			active = append(active, svc.Code)
		}
	}
	if want := []string{models.ServiceNetflix, models.ServiceHulu}; !reflect.DeepEqual(active, want) {
		t.Errorf("active = %v, want %v", active, want)
	}
}

func TestDemoResponsesAreCompressed(t *testing.T) {
	h := newDemoHarness(t, viewerEmail)
	h.seed()

	rec := h.do(http.MethodGet, "/api/demo/watchlist", http.Header{"Accept-Encoding": {"gzip"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decompressed body is not JSON: %v", err)
	}
	if len(itemTitles(body)) != 3 {
		t.Errorf("decompressed body = %s", raw)
	}

	// A client that does not ask for gzip gets plain JSON.
	if got := h.do(http.MethodGet, "/api/demo/watchlist", nil).Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q without Accept-Encoding", got)
	}
}

// Whoever registers the demo address with the identity provider must not be
// able to act as the demo account.
func TestReservedDemoAddressCannotSignIn(t *testing.T) {
	for _, email := range []string{
		DemoUserEmail,
		"Demo@WatchlistNotify.com",
		"  demo@watchlistnotify.com ",
		"DEMO@WATCHLISTNOTIFY.COM",
	} {
		h := newDemoHarness(t, email)
		h.seed()
		reads := h.reads()

		for _, tc := range []struct{ method, target string }{
			{http.MethodGet, "/api/me"},
			{http.MethodGet, "/api/watchlist"},
			{http.MethodPost, "/api/watchlist"},
		} {
			rec := h.do(tc.method, tc.target, nil)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%q: %s %s: status = %d, want 403", email, tc.method, tc.target, rec.Code)
			}
		}
		if h.store.Writes != 0 || h.reads() != reads {
			t.Errorf("%q: a refused request reached the store", email)
		}
	}
}

func TestOtherAddressesStillSignIn(t *testing.T) {
	for _, email := range []string{viewerEmail, "demo@example.com", "demo@watchlistnotify.com.example.org"} {
		h := newDemoHarness(t, email)
		h.store.AddUser(&models.User{Email: email, Region: "US"})
		if rec := h.do(http.MethodGet, "/api/watchlist", nil); rec.Code != http.StatusOK {
			t.Errorf("%q: status = %d, want 200", email, rec.Code)
		}
	}
}
