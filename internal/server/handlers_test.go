package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

const viewerEmail = "viewer@example.com"

var base = time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

// fakeTMDb answers title lookups from a map and counts them.
type fakeTMDb struct {
	mu     sync.Mutex
	titles map[string]*tmdb.TitleDetails
	err    error
	calls  int
	// hang, when set, runs and then blocks the lookup until its context
	// ends, the way a rate-limited TMDb keeps the client backing off.
	hang func()
}

func (f *fakeTMDb) GetTitle(ctx context.Context, mediaType string, id int) (*tmdb.TitleDetails, error) {
	f.mu.Lock()
	hang := f.hang
	f.mu.Unlock()
	if hang != nil {
		hang()
		<-ctx.Done()
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		return nil, ctx.Err()
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if details, ok := f.titles[models.TitleRef{MediaType: mediaType, TMDbID: id}.Key()]; ok {
		return details, nil
	}
	return nil, &tmdb.APIError{Op: "title", StatusCode: http.StatusNotFound}
}

// streamsOn makes TMDb say the title streams on the given providers, in the
// US and in Canada.
func (f *fakeTMDb) streamsOn(mediaType string, id int, providers ...tmdb.Provider) {
	if f.titles == nil {
		f.titles = make(map[string]*tmdb.TitleDetails)
	}
	region := tmdb.RegionProviders{Link: "https://example.test/watch", Flatrate: providers}
	f.titles[models.TitleRef{MediaType: mediaType, TMDbID: id}.Key()] = &tmdb.TitleDetails{
		ID: id, MediaType: mediaType, Title: "Fetched", Year: 2020, PosterPath: "/fetched.jpg",
		Providers: map[string]tmdb.RegionProviders{"US": region, "CA": region},
	}
}

// harness is the API with an in-memory store, a fake TMDb and no Auth0: the
// test router authenticates every request as viewerEmail.
type harness struct {
	t      *testing.T
	store  *storetest.Store
	tmdb   *fakeTMDb
	user   *models.User
	router *gin.Engine
	// lookupBudget shortens the upstream lookup deadline for routers built
	// after it is set.
	lookupBudget time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	t.Setenv("TMDB_REGION", "")
	gin.SetMode(gin.TestMode)

	store := storetest.New()
	fake := &fakeTMDb{}
	h := &harness{t: t, store: store, tmdb: fake}
	h.user = store.AddUser(&models.User{
		Email:  viewerEmail,
		Region: "US",
		Services: []models.ServiceSubscription{
			{Code: models.ServiceNetflix, Active: true},
			{Code: models.ServiceHulu, Active: true},
		},
	})
	h.router = h.newRouter(fake)
	return h
}

// newRouter wires the handlers under test the way RegisterRoutes does, minus
// the JWT middleware. A nil fetcher is an API without a TMDb key.
func (h *harness) newRouter(fetcher availability.Fetcher) *gin.Engine {
	refresher := availability.NewRefresher(fetcher, h.store, nil)
	s := &Server{
		db:            h.store,
		refresher:     refresher,
		lister:        watchlist.NewLister(h.store, refresher, "US"),
		defaultRegion: "US",
		lookupBudget:  h.lookupBudget,
	}
	r := gin.New()
	r.GET("/health", s.healthHandler)
	api := r.Group("/api", func(c *gin.Context) { c.Set("user_email", viewerEmail) })
	api.GET("/watchlist", s.listWatchlistItemsHandler)
	api.POST("/watchlist", s.createWatchlistItemHandler)
	api.GET("/availability/:id", s.availabilityHandler)
	api.POST("/availability/batch", s.batchAvailabilityHandler)
	return r
}

func (h *harness) do(method, target, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// get performs a request and decodes a JSON object body.
func (h *harness) get(target string, wantStatus int) map[string]any {
	h.t.Helper()
	return h.decode(h.do(http.MethodGet, target, ""), wantStatus)
}

func (h *harness) post(target, body string, wantStatus int) map[string]any {
	h.t.Helper()
	return h.decode(h.do(http.MethodPost, target, body), wantStatus)
}

func (h *harness) decode(rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
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

func (h *harness) addItem(name, typ, status string, tmdbID, daysAgo int) *models.WatchlistItem {
	return h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: name, Type: typ, Status: status, Year: 2000 + daysAgo, TMDbID: tmdbID,
		Tags: []string{"tag"}, AddedAt: base.AddDate(0, 0, -daysAgo), UpdatedAt: base,
	})
}

func (h *harness) putTitle(key string, offers ...models.ProviderOffer) {
	h.store.PutTitle(&models.Title{
		ID:            key,
		TitleMetadata: models.TitleMetadata{PosterPath: "/poster.jpg", Rating: 7.5, Runtime: 100, Genres: []string{"Drama"}},
		Providers: map[string]models.RegionSnapshot{
			"US": {Link: "https://example.test/" + key, Offers: offers, FetchedAt: base},
		},
	})
}

func offer(id int, name, access string) models.ProviderOffer {
	return models.ProviderOffer{ProviderID: id, ProviderName: name, LogoPath: "/logo.jpg", Access: access}
}

// seed gives the viewer three stored items: Matrix on Netflix (theirs),
// Thrones on HBO Max (not theirs) and Arrival on Hulu (theirs, finished).
func (h *harness) seed() {
	h.addItem("The Matrix", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 603, 1)
	h.addItem("Game of Thrones", models.WatchlistTypeShow, models.WatchlistStatusWatching, 1399, 2)
	h.addItem("Arrival", models.WatchlistTypeMovie, models.WatchlistStatusFinished, 329865, 3)
	h.putTitle("movie:603", offer(8, "Netflix", models.AccessSubscription), offer(10, "Amazon Video", models.AccessRent))
	h.putTitle("tv:1399", offer(1899, "HBO Max", models.AccessSubscription))
	h.putTitle("movie:329865", offer(15, "Hulu", models.AccessAds))
}

func itemTitles(body map[string]any) []string {
	names := []string{}
	for _, raw := range body["items"].([]any) {
		names = append(names, raw.(map[string]any)["title"].(string))
	}
	return names
}

func TestListWatchlistResponseShape(t *testing.T) {
	h := newHarness(t)
	h.seed()

	rec := h.do(http.MethodGet, "/api/watchlist", "")
	body := h.decode(rec, http.StatusOK)

	if got, want := itemTitles(body), []string{"The Matrix", "Game of Thrones", "Arrival"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	matrix := body["items"].([]any)[0].(map[string]any)

	// Every field the endpoint returned before this change is still there.
	for _, key := range []string{"id", "user_id", "title", "type", "year", "tmdb_id", "tags", "status", "added_at", "updated_at"} {
		if _, ok := matrix[key]; !ok {
			t.Errorf("item lost its %q field: %v", key, matrix)
		}
	}
	if matrix["tmdb_id"] != float64(603) || matrix["status"] != "planned" || matrix["type"] != "movie" {
		t.Errorf("original fields changed: %v", matrix)
	}
	if matrix["poster_path"] != "/poster.jpg" || matrix["rating"] != 7.5 || matrix["runtime"] != float64(100) {
		t.Errorf("title metadata missing: %v", matrix)
	}

	av := matrix["availability"].(map[string]any)
	wantProviders := []any{map[string]any{"code": "netflix", "name": "Netflix", "logo_path": "/logo.jpg", "access": []any{"subscription"}}}
	if !reflect.DeepEqual(av["providers"], wantProviders) {
		t.Errorf("providers = %v, want %v", av["providers"], wantProviders)
	}
	if av["region"] != "US" || av["link"] != "https://example.test/movie:603" || av["checked_at"] != "2026-03-15T12:00:00Z" {
		t.Errorf("availability = %v", av)
	}
	if elsewhere := av["elsewhere"].([]any); len(elsewhere) != 1 || elsewhere[0].(map[string]any)["name"] != "Amazon Video" {
		t.Errorf("elsewhere = %v", av["elsewhere"])
	}

	meta := body["meta"].(map[string]any)
	wantMeta := map[string]any{
		"limit": float64(20), "offset": float64(0), "returned": float64(3), "total": float64(3), "region": "US",
		"facets": map[string]any{
			"services":    map[string]any{"netflix": float64(1), "hulu": float64(1), "max": float64(1)},
			"available":   float64(2),
			"unavailable": float64(1),
			"status":      map[string]any{"planned": float64(1), "watching": float64(1), "finished": float64(1)},
		},
	}
	if !reflect.DeepEqual(meta, wantMeta) {
		t.Errorf("meta =\n%v\nwant\n%v", meta, wantMeta)
	}

	timing := rec.Header().Get("Server-Timing")
	for _, metric := range []string{"db;dur=", "tmdb;dur=", "total;dur="} {
		if !strings.Contains(timing, metric) {
			t.Errorf("Server-Timing = %q, want a %q metric", timing, metric)
		}
	}
	if h.tmdb.calls != 0 {
		t.Errorf("listing stored titles called TMDb %d times", h.tmdb.calls)
	}
}

func TestListWatchlistQueryParameters(t *testing.T) {
	tests := []struct {
		query string
		want  []string
	}{
		{"status=planned", []string{"The Matrix"}},
		{"status=WATCHING", []string{"Game of Thrones"}},
		{"type=show", []string{"Game of Thrones"}},
		{"search=arri", []string{"Arrival"}},
		{"sort=title", []string{"Arrival", "Game of Thrones", "The Matrix"}},
		{"sort=added_at", []string{"Arrival", "Game of Thrones", "The Matrix"}},
		{"service=hulu", []string{"Arrival"}},
		{"service=hulu&service=max", []string{"Game of Thrones", "Arrival"}},
		{"service=hulu,max", []string{"Game of Thrones", "Arrival"}},
		{"service=Hulu,%20netflix", []string{"The Matrix", "Arrival"}},
		{"available=true", []string{"The Matrix", "Arrival"}},
		{"available=1", []string{"The Matrix", "Arrival"}},
		{"available=false", []string{"Game of Thrones"}},
		{"available=0&type=movie", []string{}},
		{"limit=0", []string{"The Matrix", "Game of Thrones", "Arrival"}},
		{"with_count=1", []string{"The Matrix", "Game of Thrones", "Arrival"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			h := newHarness(t)
			h.seed()
			body := h.get("/api/watchlist?"+tt.query, http.StatusOK)
			if got := itemTitles(body); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %v, want %v", got, tt.want)
			}
			if total := body["meta"].(map[string]any)["total"]; total != float64(len(tt.want)) {
				t.Errorf("meta.total = %v, want %d", total, len(tt.want))
			}
		})
	}
}

func TestListWatchlistRejectsBadParameters(t *testing.T) {
	tests := []struct{ query, wantError string }{
		{"status=abandoned", "invalid status filter"},
		{"type=podcast", "invalid type filter"},
		{"service=blockbuster", "invalid service filter"},
		{"service=netflix,blockbuster", "invalid service filter"},
		{"available=maybe", "invalid available filter"},
		{"limit=-1", "invalid limit"},
		{"limit=ten", "invalid limit"},
		{"offset=-5", "invalid offset"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			h := newHarness(t)
			h.seed()
			body := h.get("/api/watchlist?"+tt.query, http.StatusBadRequest)
			if body["error"] != tt.wantError {
				t.Errorf("error = %v, want %q", body["error"], tt.wantError)
			}
		})
	}
}

func TestListWatchlistPaging(t *testing.T) {
	h := newHarness(t)
	h.seed()

	tests := []struct {
		query      string
		want       []string
		nextOffset any // nil when there is no further page
	}{
		{"limit=2", []string{"The Matrix", "Game of Thrones"}, float64(2)},
		{"limit=2&offset=2", []string{"Arrival"}, nil},
		{"limit=3", []string{"The Matrix", "Game of Thrones", "Arrival"}, nil},
		{"limit=1&offset=1", []string{"Game of Thrones"}, float64(2)},
		{"limit=2&offset=7", []string{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			body := h.get("/api/watchlist?"+tt.query, http.StatusOK)
			if got := itemTitles(body); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %v, want %v", got, tt.want)
			}
			meta := body["meta"].(map[string]any)
			if meta["total"] != float64(3) || meta["returned"] != float64(len(tt.want)) {
				t.Errorf("meta = %v", meta)
			}
			if next, present := meta["next_offset"]; next != tt.nextOffset || present != (tt.nextOffset != nil) {
				t.Errorf("next_offset = %v (present: %v), want %v", next, present, tt.nextOffset)
			}
		})
	}
}

func TestListWatchlistEmpty(t *testing.T) {
	h := newHarness(t)
	rec := h.do(http.MethodGet, "/api/watchlist", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Errorf("status %d, body %s; want an empty items array", rec.Code, rec.Body)
	}
}

func TestListWatchlistFetchesMissingTitles(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.addItem("New Arrival", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 4242, 0)
	h.addItem("Gone From TMDb", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 4040, 5)
	h.tmdb.streamsOn("movie", 4242, tmdb.Provider{ProviderID: 15, ProviderName: "Hulu"})

	body := h.get("/api/watchlist?limit=0", http.StatusOK)
	items := body["items"].([]any)
	fetched := items[0].(map[string]any)
	if fetched["title"] != "New Arrival" || fetched["availability"] == nil {
		t.Fatalf("missing title was not fetched: %v", fetched)
	}
	if gone := items[4].(map[string]any); gone["availability"] != nil {
		t.Errorf("a title TMDb does not have should have null availability, got %v", gone["availability"])
	}
	if _, ok := h.store.Title("movie:4242").Snapshot("US"); !ok {
		t.Error("the fetched title was not stored as a baseline")
	}
	if events := h.store.Events(); len(events) != 0 {
		t.Errorf("a baseline produced %d events", len(events))
	}

	// The second request finds both in the store, the missing one as a tombstone.
	calls := h.tmdb.calls
	h.get("/api/watchlist?limit=0", http.StatusOK)
	if h.tmdb.calls != calls {
		t.Errorf("second list made %d more TMDb calls", h.tmdb.calls-calls)
	}
}

func TestListWatchlistDatabaseError(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.store.Err = errors.New("database down")
	// The user lookup fails first; either way the client sees a 500.
	h.get("/api/watchlist", http.StatusInternalServerError)
}

func TestCreateWatchlistItemStoresTheTitle(t *testing.T) {
	h := newHarness(t)
	h.tmdb.streamsOn("tv", 1399, tmdb.Provider{ProviderID: 8, ProviderName: "Netflix"})

	created := h.post("/api/watchlist", `{"title":"Game of Thrones","type":"show","tmdb_id":1399}`, http.StatusCreated)
	if created["title"] != "Game of Thrones" || created["status"] != "planned" || created["id"] == "" {
		t.Errorf("created item = %v", created)
	}
	if _, ok := h.store.Title("tv:1399").Snapshot("US"); !ok {
		t.Fatal("adding an item did not store its title")
	}

	calls := h.tmdb.calls
	body := h.get("/api/watchlist", http.StatusOK)
	if av := body["items"].([]any)[0].(map[string]any)["availability"]; av == nil {
		t.Error("the new item has no availability on the next list")
	}
	if h.tmdb.calls != calls {
		t.Error("listing after a create called TMDb again")
	}
}

func TestCreateWatchlistItemSurvivesTMDbFailure(t *testing.T) {
	h := newHarness(t)
	h.tmdb.err = errors.New("tmdb unreachable")
	h.post("/api/watchlist", `{"title":"Arrival","type":"movie","tmdb_id":329865}`, http.StatusCreated)
	if h.store.Title("movie:329865") != nil {
		t.Error("a failed fetch stored a title")
	}

	keyless := newHarness(t)
	keyless.router = keyless.newRouter(nil)
	keyless.post("/api/watchlist", `{"title":"Arrival","type":"movie","tmdb_id":329865}`, http.StatusCreated)
}

func TestAvailabilityFromTheStore(t *testing.T) {
	h := newHarness(t)
	h.seed()

	body := h.get("/api/availability/603?type=movie", http.StatusOK)
	want := map[string]any{
		"region": "US",
		"providers": []any{map[string]any{
			"code": "netflix", "name": "Netflix", "logo_path": "/logo.jpg",
			"link": "https://example.test/movie:603", "access": []any{"subscription"},
		}},
		"unmatched_user_services": []any{"hulu"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Errorf("body =\n%v\nwant\n%v", body, want)
	}
	if h.tmdb.calls != 0 {
		t.Errorf("a stored title cost %d TMDb calls", h.tmdb.calls)
	}

	// Streaming only on a service the user does not have.
	body = h.get("/api/availability/1399?type=tv", http.StatusOK)
	if providers := body["providers"].([]any); len(providers) != 0 {
		t.Errorf("providers = %v, want none", providers)
	}
	if got := body["unmatched_user_services"]; !reflect.DeepEqual(got, []any{"hulu", "netflix"}) {
		t.Errorf("unmatched_user_services = %v", got)
	}
}

func TestAvailabilityFetchesAndStoresAMiss(t *testing.T) {
	h := newHarness(t)
	h.tmdb.streamsOn("movie", 603, tmdb.Provider{ProviderID: 8, ProviderName: "Netflix", LogoPath: "/n.jpg"})

	body := h.get("/api/availability/603?type=movie", http.StatusOK)
	if providers := body["providers"].([]any); len(providers) != 1 || providers[0].(map[string]any)["code"] != "netflix" {
		t.Fatalf("providers = %v", body["providers"])
	}
	if _, ok := h.store.Title("movie:603").Snapshot("US"); !ok {
		t.Fatal("the miss was not stored")
	}

	h.get("/api/availability/603?type=movie", http.StatusOK)
	if h.tmdb.calls != 1 {
		t.Errorf("TMDb was called %d times for two requests, want 1", h.tmdb.calls)
	}
}

func TestAvailabilityForAnotherRegionIsNotStored(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.tmdb.streamsOn("movie", 603, tmdb.Provider{ProviderID: 15, ProviderName: "Hulu"})

	body := h.get("/api/availability/603?type=movie&region=ca", http.StatusOK)
	if body["region"] != "CA" {
		t.Errorf("region = %v, want CA", body["region"])
	}
	if providers := body["providers"].([]any); len(providers) != 1 || providers[0].(map[string]any)["code"] != "hulu" {
		t.Errorf("providers = %v, want the Canadian answer", body["providers"])
	}
	if _, ok := h.store.Title("movie:603").Snapshot("CA"); ok {
		t.Error("a snapshot was stored for a region nothing refreshes")
	}
	if events := h.store.Events(); len(events) != 0 {
		t.Errorf("an ad-hoc region lookup recorded %d events", len(events))
	}
}

func TestAvailabilityErrors(t *testing.T) {
	t.Run("bad requests", func(t *testing.T) {
		h := newHarness(t)
		for _, target := range []string{
			"/api/availability/603",
			"/api/availability/603?type=show",
			"/api/availability/abc?type=movie",
			"/api/availability/0?type=movie",
			"/api/availability/-4?type=tv",
		} {
			h.get(target, http.StatusBadRequest)
		}
	})

	t.Run("upstream failure", func(t *testing.T) {
		h := newHarness(t)
		h.tmdb.err = errors.New("tmdb unreachable")
		h.get("/api/availability/603?type=movie", http.StatusBadGateway)
	})

	t.Run("title unknown to TMDb", func(t *testing.T) {
		h := newHarness(t)
		body := h.get("/api/availability/603?type=movie", http.StatusOK)
		if providers, ok := body["providers"].([]any); !ok || len(providers) != 0 {
			t.Errorf("providers = %#v, want an empty array", body["providers"])
		}
		// The tombstone answers the next request without TMDb.
		h.get("/api/availability/603?type=movie", http.StatusOK)
		if h.tmdb.calls != 1 {
			t.Errorf("TMDb was called %d times, want 1", h.tmdb.calls)
		}
	})

	t.Run("no TMDb key", func(t *testing.T) {
		h := newHarness(t)
		h.seed()
		h.router = h.newRouter(nil)
		h.get("/api/availability/603?type=movie", http.StatusOK) // stored: still served
		h.get("/api/availability/777?type=movie", http.StatusServiceUnavailable)
	})

	t.Run("database failure", func(t *testing.T) {
		h := newHarness(t)
		h.store.Err = errors.New("database down")
		h.get("/api/availability/603?type=movie", http.StatusInternalServerError)
	})
}

// A rate-limited TMDb keeps the client backing off for longer than the
// server's write timeout. The handler has to answer inside a deadline of its
// own instead of holding the connection until it is cut.
func TestAvailabilityLookupHasADeadline(t *testing.T) {
	slow := func(t *testing.T) *harness {
		h := newHarness(t)
		h.lookupBudget = 50 * time.Millisecond
		h.router = h.newRouter(h.tmdb)
		h.tmdb.hang = func() {}
		return h
	}
	// timed runs a request against a hanging TMDb and fails if the handler
	// waited for anything like the client's full retry schedule.
	timed := func(t *testing.T, h *harness, target string, wantStatus int) map[string]any {
		t.Helper()
		start := time.Now()
		body := h.get(target, wantStatus)
		if elapsed := time.Since(start); elapsed > 3*time.Second {
			t.Errorf("the request took %v, want it cut off at the lookup deadline", elapsed)
		}
		return body
	}

	t.Run("nothing stored", func(t *testing.T) {
		h := slow(t)
		body := timed(t, h, "/api/availability/603?type=movie", http.StatusGatewayTimeout)
		if body["error"] != "availability lookup timed out" {
			t.Errorf("body = %v", body)
		}
		if h.store.Title("movie:603") != nil {
			t.Error("a lookup that timed out stored something")
		}
	})

	t.Run("a region that is not stored", func(t *testing.T) {
		h := slow(t)
		timed(t, h, "/api/availability/603?type=movie&region=ca", http.StatusGatewayTimeout)
	})

	t.Run("stored by someone else meanwhile", func(t *testing.T) {
		h := slow(t)
		h.tmdb.hang = func() { h.putTitle("movie:603", offer(8, "Netflix", models.AccessSubscription)) }

		body := timed(t, h, "/api/availability/603?type=movie", http.StatusOK)
		if providers := body["providers"].([]any); len(providers) != 1 || providers[0].(map[string]any)["code"] != "netflix" {
			t.Errorf("providers = %v, want what the store holds", body["providers"])
		}
	})

	t.Run("a stored title never waits on TMDb", func(t *testing.T) {
		h := slow(t)
		h.seed()
		timed(t, h, "/api/availability/603?type=movie", http.StatusOK)
		if h.tmdb.calls != 0 {
			t.Errorf("a stored title cost %d TMDb calls", h.tmdb.calls)
		}
	})
}

// Adding an item fetches its availability on a best-effort budget; a TMDb
// that never answers must not hold the response.
func TestCreateWatchlistItemDoesNotWaitOnASlowTMDb(t *testing.T) {
	h := newHarness(t)
	h.tmdb.hang = func() {}

	start := time.Now()
	h.post("/api/watchlist", `{"title":"The Matrix","type":"movie","tmdb_id":603}`, http.StatusCreated)

	if elapsed := time.Since(start); elapsed > createEnsureBudget+2*time.Second {
		t.Errorf("the request took %v, want it bounded by the %v fetch budget", elapsed, createEnsureBudget)
	}
}

func TestBatchAvailability(t *testing.T) {
	h := newHarness(t)
	h.seed()
	h.tmdb.streamsOn("movie", 4242, tmdb.Provider{ProviderID: 15, ProviderName: "Hulu"})

	body := h.post("/api/availability/batch", `{"items":[
		{"id":603,"type":"movie"},
		{"id":1399,"type":"tv"},
		{"id":4242,"type":"movie"},
		{"id":4040,"type":"movie"},
		{"id":603,"type":"movie"}
	]}`, http.StatusOK)

	if body["region"] != "US" {
		t.Errorf("region = %v", body["region"])
	}
	results := body["results"].(map[string]any)
	if len(results) != 4 {
		t.Fatalf("results has %d entries, want 4 (duplicates collapse): %v", len(results), results)
	}
	codes := func(key string) []string {
		entry, ok := results[key].(map[string]any)
		if !ok {
			t.Fatalf("no result under %q: %v", key, results)
		}
		out := []string{}
		for _, p := range entry["providers"].([]any) {
			out = append(out, p.(map[string]any)["code"].(string))
		}
		return out
	}
	if got := codes("movie_603"); !reflect.DeepEqual(got, []string{"netflix"}) {
		t.Errorf("movie_603 providers = %v", got)
	}
	if got := codes("tv_1399"); len(got) != 0 {
		t.Errorf("tv_1399 providers = %v, want none", got)
	}
	if got := codes("movie_4242"); !reflect.DeepEqual(got, []string{"hulu"}) {
		t.Errorf("movie_4242 (fetched on demand) providers = %v", got)
	}
	// Unresolved: no providers, every service unmatched.
	if got := codes("movie_4040"); len(got) != 0 {
		t.Errorf("movie_4040 providers = %v, want none", got)
	}
	if got := results["movie_4040"].(map[string]any)["unmatched_user_services"]; !reflect.DeepEqual(got, []any{"hulu", "netflix"}) {
		t.Errorf("movie_4040 unmatched_user_services = %v", got)
	}
	if link := results["movie_603"].(map[string]any)["providers"].([]any)[0].(map[string]any)["link"]; link != "https://example.test/movie:603" {
		t.Errorf("provider link = %v", link)
	}
}

func TestBatchAvailabilityRejectsBadRequests(t *testing.T) {
	tooMany := &bytes.Buffer{}
	tooMany.WriteString(`{"items":[`)
	for i := 1; i <= maxBatchSize+1; i++ {
		if i > 1 {
			tooMany.WriteByte(',')
		}
		fmt.Fprintf(tooMany, `{"id":%d,"type":"movie"}`, i)
	}
	tooMany.WriteString(`]}`)

	tests := []struct{ name, body string }{
		{"not JSON", `nope`},
		{"no items", `{}`},
		{"empty items", `{"items":[]}`},
		{"bad type", `{"items":[{"id":1,"type":"show"}]}`},
		{"missing id", `{"items":[{"type":"movie"}]}`},
		{"negative id", `{"items":[{"id":-1,"type":"movie"}]}`},
		{"too many", tooMany.String()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.post("/api/availability/batch", tt.body, http.StatusBadRequest)
			if h.tmdb.calls != 0 {
				t.Errorf("a rejected batch called TMDb %d times", h.tmdb.calls)
			}
		})
	}
}

func TestBatchAvailabilityAtTheLimitIsAccepted(t *testing.T) {
	h := newHarness(t)
	var body bytes.Buffer
	body.WriteString(`{"items":[`)
	for i := 1; i <= maxBatchSize; i++ {
		if i > 1 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `{"id":%d,"type":"movie"}`, i)
		h.putTitle(models.TitleRef{MediaType: "movie", TMDbID: i}.Key(), offer(8, "Netflix", models.AccessSubscription))
	}
	body.WriteString(`]}`)
	h.store.Queries = map[string]int{}

	resp := h.post("/api/availability/batch", body.String(), http.StatusOK)
	if got := len(resp["results"].(map[string]any)); got != maxBatchSize {
		t.Errorf("%d results, want %d", got, maxBatchSize)
	}
	if h.store.Queries["GetTitles"] != 1 || h.tmdb.calls != 0 {
		t.Errorf("a stored batch cost %d title queries and %d TMDb calls, want 1 and 0", h.store.Queries["GetTitles"], h.tmdb.calls)
	}
}

func TestHealth(t *testing.T) {
	h := newHarness(t)
	if body := h.get("/health", http.StatusOK); body["status"] != "ok" {
		t.Errorf("healthy body = %v", body)
	}
	h.store.Err = errors.New("database down")
	body := h.get("/health", http.StatusServiceUnavailable)
	if body["status"] != "unavailable" || strings.Contains(fmt.Sprint(body), "database down") {
		t.Errorf("unhealthy body = %v; want status unavailable and no internal error text", body)
	}
}
