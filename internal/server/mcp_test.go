package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/neilsmahajan/watchlist-notify/internal/authtest"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

const (
	otherEmail     = "other@example.com"
	frontendOrigin = "https://watchlistnotify.com"
	readScope      = middleware.ScopeWatchlistRead
	readWrite      = middleware.ScopeWatchlistRead + " " + middleware.ScopeWatchlistWrite
)

// mcpHarness is the whole API as RegisterRoutes builds it, with the MCP
// endpoint on, served over HTTP: an in-memory store, a fake Auth0 tenant
// that signs real tokens, a fake TMDb, and an in-memory search cache.
type mcpHarness struct {
	t      *testing.T
	store  *storetest.Store
	issuer *authtest.Issuer
	// titles answers title lookups; search answers TMDb's search endpoints.
	titles *fakeTMDb
	search *fakeSearch
	cache  *memoryCache
	viewer *models.User
	other  *models.User

	baseURL  string
	resource string // the MCP endpoint, and the audience of its tokens
	metadata string
	restAud  string
}

func newMCPHarness(t *testing.T) *mcpHarness {
	t.Helper()
	t.Setenv("TMDB_REGION", "")
	t.Setenv("TMDB_BEARER_TOKEN", "")
	gin.SetMode(gin.TestMode)

	h := &mcpHarness{
		t:      t,
		store:  storetest.New(),
		issuer: authtest.NewIssuer(t),
		titles: &fakeTMDb{},
		search: newFakeSearch(t),
		cache:  &memoryCache{entries: map[string]cache.SearchValue{}},
	}
	h.viewer = h.store.AddUser(&models.User{
		Email:  viewerEmail,
		Region: "US",
		Services: []models.ServiceSubscription{
			{Code: models.ServiceNetflix, Active: true},
			{Code: models.ServiceHulu, Active: true},
		},
	})
	h.other = h.store.AddUser(&models.User{Email: otherEmail, Region: "US"})

	// The resource URL names the address the server listens on, so the
	// listener has to exist before the routes are built.
	srv := httptest.NewUnstartedServer(nil)
	h.baseURL = "http://" + srv.Listener.Addr().String()
	h.resource = h.baseURL + mcpPath
	h.metadata = h.baseURL + mcpMetadataPath + mcpPath
	h.restAud = h.baseURL + "/api"

	searchClient, err := tmdb.New("test-key", tmdb.WithBaseURL(h.search.server.URL))
	if err != nil {
		t.Fatal(err)
	}
	restAuth, err := middleware.EnsureValidTokenFrom(h.issuer.URL(), h.restAud)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := h.api(searchClient).routes(routeConfig{
		origins:  []string{frontendOrigin},
		restAuth: restAuth,
		mcp:      h.config(),
	})
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	srv.Config.Handler = handler
	srv.Start()
	t.Cleanup(srv.Close)
	return h
}

func (h *mcpHarness) api(search *tmdb.Client) *Server {
	var fetcher availability.Fetcher = h.titles
	refresher := availability.NewRefresher(fetcher, h.store, nil)
	return &Server{
		db:            h.store,
		cache:         h.cache,
		tmdb:          search,
		refresher:     refresher,
		lister:        watchlist.NewLister(h.store, refresher, "US"),
		defaultRegion: "US",
	}
}

func (h *mcpHarness) config() *mcpConfig {
	return &mcpConfig{resourceURL: h.resource, metadataURL: h.metadata, issuer: h.issuer.URL()}
}

// token mints an access token for the MCP resource.
func (h *mcpHarness) token(email, scope string) string {
	return h.issuer.Token(h.resource, email, scope)
}

// seed gives the viewer three stored items: Matrix on Netflix (theirs),
// Thrones on HBO Max (not theirs) and Arrival on Netflix and Hulu (both
// theirs, finished). The other user gets one item of their own.
func (h *mcpHarness) seed() (matrix, thrones, arrival, others *models.WatchlistItem) {
	add := func(user *models.User, name, typ, status string, year, tmdbID, daysAgo int, tags ...string) *models.WatchlistItem {
		return h.store.AddItem(&models.WatchlistItem{
			UserID: user.ID, Title: name, Type: typ, Status: status, Year: year, TMDbID: tmdbID, Tags: tags,
			AddedAt: base.AddDate(0, 0, -daysAgo), UpdatedAt: base,
		})
	}
	matrix = add(h.viewer, "The Matrix", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 1999, 603, 1, "sci-fi")
	thrones = add(h.viewer, "Game of Thrones", models.WatchlistTypeShow, models.WatchlistStatusWatching, 2011, 1399, 2)
	arrival = add(h.viewer, "Arrival", models.WatchlistTypeMovie, models.WatchlistStatusFinished, 2016, 329865, 3)
	others = add(h.other, "Heat", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 1995, 949, 1, "theirs")

	put := func(key, name string, year int, offers ...models.ProviderOffer) {
		h.store.PutTitle(&models.Title{
			ID: key,
			TitleMetadata: models.TitleMetadata{
				Title: name, Year: year, PosterPath: "/poster.jpg", Overview: "A long synopsis that no tool should return.",
			},
			Providers: map[string]models.RegionSnapshot{"US": {Offers: offers, FetchedAt: base}},
		})
	}
	put("movie:603", "The Matrix", 1999, offer(8, "Netflix", models.AccessSubscription), offer(10, "Amazon Video", models.AccessRent))
	put("tv:1399", "Game of Thrones", 2011, offer(1899, "HBO Max", models.AccessSubscription))
	put("movie:329865", "Arrival", 2016, offer(8, "Netflix", models.AccessSubscription), offer(15, "Hulu", models.AccessAds))
	put("movie:949", "Heat", 1995, offer(8, "Netflix", models.AccessSubscription))
	return matrix, thrones, arrival, others
}

// --- raw HTTP ---

const listToolsRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

func callRequest(tool, arguments string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, arguments)
}

// request sends one HTTP request to the API without following redirects or
// asking for compression on its own.
func (h *mcpHarness) request(method, path, body string, edit func(*http.Request)) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(method, h.baseURL+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}
	if edit != nil {
		edit(req)
	}
	client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
	resp, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// post sends a JSON-RPC message to the MCP endpoint.
func (h *mcpHarness) post(body string, edit func(*http.Request)) *http.Response {
	h.t.Helper()
	return h.request(http.MethodPost, mcpPath, body, edit)
}

func withToken(token string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func bodyOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (h *mcpHarness) challenge(errorCode, description string) string {
	prefix := "Bearer "
	if errorCode != "" {
		prefix += fmt.Sprintf(`error=%q, error_description=%q, `, errorCode, description)
	}
	return prefix + fmt.Sprintf(`resource_metadata=%q, scope="watchlist:read watchlist:write"`, h.metadata)
}

// --- the SDK's client ---

type bearerTransport struct{ token string }

func (b bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(req)
}

// connect opens an MCP session against the endpoint with the SDK's own
// client, as the given user.
func (h *mcpHarness) connect(email, scope string) *mcp.ClientSession {
	h.t.Helper()
	return connectMCP(h.t, h.resource, h.token(email, scope))
}

func connectMCP(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: bearerTransport{token}},
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// call runs a tool and returns its result, failing the test on a protocol
// error. A tool error comes back in the result.
func call(t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if arguments == nil {
		arguments = map[string]any{}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	return result
}

// succeed runs a tool that must succeed and decodes its structured result.
func succeed[Out any](t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) (Out, *mcp.CallToolResult) {
	t.Helper()
	result := call(t, session, tool, arguments)
	if result.IsError {
		t.Fatalf("%s failed: %s", tool, textOf(result, 0))
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("%s: structured content %s: %v", tool, raw, err)
	}
	return out, result
}

// refuse runs a tool that must fail and returns the message it failed with.
func refuse(t *testing.T, session *mcp.ClientSession, tool string, arguments map[string]any) string {
	t.Helper()
	result := call(t, session, tool, arguments)
	if !result.IsError {
		t.Fatalf("%s succeeded: %s", tool, textOf(result, 0))
	}
	return textOf(result, 0)
}

func textOf(result *mcp.CallToolResult, index int) string {
	if index >= len(result.Content) {
		return ""
	}
	text, _ := result.Content[index].(*mcp.TextContent)
	if text == nil {
		return ""
	}
	return text.Text
}

func titlesOf(items []mcpItem) []string {
	names := []string{}
	for _, item := range items {
		names = append(names, item.Title)
	}
	return names
}

// --- fakes ---

// fakeSearch is TMDb's search API: a canned page per media type.
type fakeSearch struct {
	server *httptest.Server
	mu     sync.Mutex
	pages  map[string]string // "movie" | "tv" -> response body
	hits   map[string]int
	status int // when set, every search answers with it
}

func newFakeSearch(t *testing.T) *fakeSearch {
	f := &fakeSearch{pages: map[string]string{}, hits: map[string]int{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType := strings.TrimPrefix(r.URL.Path, "/search/")
		f.mu.Lock()
		f.hits[mediaType]++
		status, page := f.status, f.pages[mediaType]
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		if page == "" {
			page = `{"page":1,"total_pages":1,"results":[]}`
		}
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(f.server.Close)
	return f
}

// finds sets what a search of one media type ("movie" or "tv") returns,
// whatever the query. An empty page means no results.
func (f *fakeSearch) finds(mediaType, page string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[mediaType] = page
}

// fails makes every search answer with the status; 0 ends that.
func (f *fakeSearch) fails(status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

// requests is how many searches TMDb has been asked for, of the given media
// types or of both.
func (f *fakeSearch) requests(mediaTypes ...string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(mediaTypes) == 0 {
		mediaTypes = []string{"movie", "tv"}
	}
	total := 0
	for _, mediaType := range mediaTypes {
		total += f.hits[mediaType]
	}
	return total
}

// memoryCache is the search cache without Redis.
type memoryCache struct {
	mu      sync.Mutex
	entries map[string]cache.SearchValue
}

func (m *memoryCache) GetSearchResults(_ context.Context, key cache.SearchKey) (*cache.SearchValue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value, ok := m.entries[key.String()]; ok {
		return &value, nil
	}
	return nil, cache.ErrMiss
}

func (m *memoryCache) SetSearchResults(_ context.Context, key cache.SearchKey, value cache.SearchValue) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries[key.String()] = value
	return nil
}

// --- configuration and routing ---

func TestLoadMCPConfig(t *testing.T) {
	cfg, err := loadMCPConfig(" https://api.watchlistnotify.com/mcp ", "tenant.us.auth0.com")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.resourceURL != "https://api.watchlistnotify.com/mcp" ||
		cfg.metadataURL != "https://api.watchlistnotify.com/.well-known/oauth-protected-resource/mcp" ||
		cfg.issuer.String() != "https://tenant.us.auth0.com/" ||
		cfg.origin() != "https://api.watchlistnotify.com" {
		t.Errorf("config = %+v (issuer %s)", cfg, cfg.issuer)
	}

	if cfg, err := loadMCPConfig("  ", "tenant.us.auth0.com"); cfg != nil || err != nil {
		t.Errorf("no resource URL: config = %v, err = %v; want the endpoint off", cfg, err)
	}

	for name, resource := range map[string]string{
		"not a URL":        "api.watchlistnotify.com/mcp",
		"another scheme":   "ftp://api.watchlistnotify.com/mcp",
		"another path":     "https://api.watchlistnotify.com/",
		"a deeper path":    "https://api.watchlistnotify.com/mcp/v1",
		"a trailing slash": "https://api.watchlistnotify.com/mcp/",
		"a query":          "https://api.watchlistnotify.com/mcp?x=1",
		"an empty query":   "https://api.watchlistnotify.com/mcp?",
		"a fragment":       "https://api.watchlistnotify.com/mcp#tools",
		"credentials":      "https://user:pass@api.watchlistnotify.com/mcp",
	} {
		if cfg, err := loadMCPConfig(resource, "tenant.us.auth0.com"); err == nil {
			t.Errorf("%s: accepted %q as %+v", name, resource, cfg)
		}
	}
	if _, err := loadMCPConfig("https://api.watchlistnotify.com/mcp", ""); err == nil {
		t.Error("a resource URL without an Auth0 domain was accepted")
	}
}

func TestMCPRoutesAreAbsentUnlessConfigured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newMCPHarness(t)
	handler, err := h.api(nil).routes(routeConfig{
		origins:  []string{frontendOrigin},
		restAuth: func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{mcpPath, mcpMetadataPath, mcpMetadataPath + mcpPath} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(method, target, strings.NewReader(listToolsRequest)))
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404", method, target, rec.Code)
			}
		}
	}
}

func TestMCPMetadataDocuments(t *testing.T) {
	h := newMCPHarness(t)
	for _, target := range []string{mcpMetadataPath + mcpPath, mcpMetadataPath} {
		// From a page on an origin the REST API would refuse, asking for gzip.
		resp := h.request(http.MethodGet, target, "", func(r *http.Request) {
			r.Header.Set("Origin", "https://some-mcp-client.example")
			r.Header.Set("Accept-Encoding", "gzip")
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", target, resp.StatusCode, bodyOf(t, resp))
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(bodyOf(t, resp)), &doc); err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		want := map[string]any{
			"resource":                 h.resource,
			"authorization_servers":    []any{h.issuer.URL().String()},
			"scopes_supported":         []any{"watchlist:read", "watchlist:write"},
			"bearer_methods_supported": []any{"header"},
			"resource_name":            "Watchlist Notify",
		}
		if !reflect.DeepEqual(doc, want) {
			t.Errorf("GET %s =\n%v\nwant\n%v", target, doc, want)
		}
		if !strings.HasSuffix(h.issuer.URL().String(), "/") {
			t.Errorf("the issuer %q must be advertised as tokens name it, with the trailing slash", h.issuer.URL())
		}
		header := resp.Header
		if header.Get("Access-Control-Allow-Origin") != "*" || header.Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("GET %s: CORS headers = %v; want any origin, no credentials", target, header)
		}
		if header.Get("Content-Encoding") != "" || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
			t.Errorf("GET %s: Content-Encoding = %q, Content-Type = %q", target, header.Get("Content-Encoding"), header.Get("Content-Type"))
		}

		preflight := h.request(http.MethodOptions, target, "", func(r *http.Request) {
			r.Header.Set("Origin", "https://some-mcp-client.example")
			r.Header.Set("Access-Control-Request-Method", "GET")
		})
		if preflight.StatusCode != http.StatusNoContent || preflight.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("OPTIONS %s = %d, headers %v", target, preflight.StatusCode, preflight.Header)
		}
	}
}

// --- authorization ---

func TestMCPChallengesARequestWithoutAToken(t *testing.T) {
	h := newMCPHarness(t)
	valid := h.token(viewerEmail, readWrite)

	for name, edit := range map[string]func(*http.Request){
		"nothing": nil,
		// The REST API accepts its token in this cookie. The MCP endpoint
		// must not: a cookie is sent by the browser, whoever wrote the page.
		"a valid token in the session cookie": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "auth_token", Value: valid})
		},
		"a valid token in the query": func(r *http.Request) {
			r.URL.RawQuery = "access_token=" + valid
		},
	} {
		resp := h.post(listToolsRequest, edit)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", name, resp.StatusCode)
		}
		if got, want := resp.Header.Get("WWW-Authenticate"), h.challenge("", ""); got != want {
			t.Errorf("%s: WWW-Authenticate =\n%s\nwant\n%s", name, got, want)
		}
	}
}

func TestMCPRefusesTokensItShouldNot(t *testing.T) {
	h := newMCPHarness(t)
	otherIssuer := authtest.NewIssuer(t)
	claims := func(edit func(authtest.Claims)) string {
		c := h.issuer.Claims(h.resource, viewerEmail, readWrite)
		edit(c)
		return h.issuer.Sign(c)
	}
	invalid := h.challenge("invalid_token", "The access token is invalid or has expired.")

	tests := []struct {
		name      string
		token     string
		status    int
		challenge string
	}{
		{"issued for the REST API", h.issuer.Token(h.restAud, viewerEmail, readWrite), 401, invalid},
		{"expired", claims(func(c authtest.Claims) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() }), 401, invalid},
		{"from another issuer", otherIssuer.Token(h.resource, viewerEmail, readWrite), 401, invalid},
		{"naming another issuer", claims(func(c authtest.Claims) { c["iss"] = "https://evil.example/" }), 401, invalid},
		{"email not verified", claims(func(c authtest.Claims) { c[authtest.ClaimEmailVerified] = false }), 401, invalid},
		{"no email", claims(func(c authtest.Claims) { delete(c, authtest.ClaimEmail) }), 401, invalid},
		{"no scopes", h.token(viewerEmail, ""), 403,
			h.challenge("insufficient_scope", "This resource needs the watchlist:read scope.")},
		{"the write scope alone", h.token(viewerEmail, middleware.ScopeWatchlistWrite), 403,
			h.challenge("insufficient_scope", "This resource needs the watchlist:read scope.")},
		// A plain refusal: signing in again as the same account cannot help.
		{"the reserved demo account", h.token(DemoUserEmail, readWrite), 403, ""},
		{"the reserved demo account, respelled", h.token(" Demo@WatchlistNotify.com", readWrite), 403, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, body := range []string{listToolsRequest, callRequest("list_watchlist", "{}")} {
				resp := h.post(body, withToken(tt.token))
				if resp.StatusCode != tt.status {
					t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, tt.status, bodyOf(t, resp))
				}
				if got := resp.Header.Get("WWW-Authenticate"); got != tt.challenge {
					t.Errorf("WWW-Authenticate =\n%s\nwant\n%s", got, tt.challenge)
				}
				if text := bodyOf(t, resp); strings.Contains(text, tt.token) || strings.Contains(text, "The Matrix") {
					t.Errorf("the refusal leaks: %s", text)
				}
			}
		})
	}
	if users := h.store.Queries["UpsertUser"]; users != 0 {
		t.Errorf("a refused token created %d accounts", users)
	}
}

// One tenant signs the tokens of both APIs with the same key. Only the
// audience tells them apart, in both directions.
func TestMCPAndRESTTokensAreNotInterchangeable(t *testing.T) {
	h := newMCPHarness(t)
	h.seed()
	rest := h.issuer.Token(h.restAud, viewerEmail, readWrite)
	forMCP := h.token(viewerEmail, readWrite)

	if resp := h.request(http.MethodGet, "/api/watchlist", "", withToken(rest)); resp.StatusCode != http.StatusOK {
		t.Fatalf("a REST token on the REST API = %d: %s", resp.StatusCode, bodyOf(t, resp))
	}
	if resp := h.request(http.MethodGet, "/api/watchlist", "", withToken(forMCP)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an MCP token on the REST API = %d, want 401", resp.StatusCode)
	}
	if resp := h.post(listToolsRequest, withToken(forMCP)); resp.StatusCode != http.StatusOK {
		t.Fatalf("an MCP token on the MCP endpoint = %d: %s", resp.StatusCode, bodyOf(t, resp))
	}
	if resp := h.post(listToolsRequest, withToken(rest)); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("a REST token on the MCP endpoint = %d, want 401", resp.StatusCode)
	}
}

func TestMCPWriteToolsNeedTheWriteScope(t *testing.T) {
	h := newMCPHarness(t)
	matrix, _, _, _ := h.seed()
	h.titles.streamsOn("movie", 27205)
	readOnly := h.token(viewerEmail, readScope)
	before := len(h.store.Items(h.viewer.ID))

	calls := map[string]string{
		"add_to_watchlist":      callRequest("add_to_watchlist", `{"tmdb_id":27205,"type":"movie"}`),
		"update_watchlist_item": callRequest("update_watchlist_item", fmt.Sprintf(`{"id":%q,"status":"finished"}`, matrix.ID.Hex())),
		"remove_from_watchlist": callRequest("remove_from_watchlist", fmt.Sprintf(`{"id":%q}`, matrix.ID.Hex())),
	}
	unchanged := func(t *testing.T) {
		t.Helper()
		items := h.store.Items(h.viewer.ID)
		if len(items) != before || h.store.Writes != 0 {
			t.Fatalf("the watchlist changed: %d items (was %d), %d writes", len(items), before, h.store.Writes)
		}
	}

	t.Run("the endpoint answers 403 insufficient_scope", func(t *testing.T) {
		for tool, body := range calls {
			want := h.challenge("insufficient_scope", "The "+tool+" tool needs the watchlist:write scope.")
			// On its own, and inside a batch as older protocol versions allow.
			for _, sent := range []string{body, "[" + listToolsRequest + "," + body + "]"} {
				resp := h.post(sent, withToken(readOnly))
				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("%s: status = %d, want 403; body: %s", tool, resp.StatusCode, bodyOf(t, resp))
				}
				if got := resp.Header.Get("WWW-Authenticate"); got != want {
					t.Errorf("%s: WWW-Authenticate =\n%s\nwant\n%s", tool, got, want)
				}
			}
		}
		unchanged(t)
	})

	t.Run("a read tool still works", func(t *testing.T) {
		out, _ := succeed[listWatchlistOutput](t, h.connect(viewerEmail, readScope), "list_watchlist", nil)
		if out.Total != 3 {
			t.Errorf("total = %d, want 3", out.Total)
		}
	})

	// Without the check in front, as if a request had slipped its tool name
	// past it: the tool itself refuses.
	t.Run("the tool refuses on its own", func(t *testing.T) {
		guard, err := middleware.NewMCPAuth(middleware.MCPAuthConfig{
			Issuer: h.issuer.URL(), ResourceURL: h.resource, MetadataURL: h.metadata,
		})
		if err != nil {
			t.Fatal(err)
		}
		transport, _ := h.api(nil).mcpTransport()
		unguarded := httptest.NewServer(guard.RequireToken(transport))
		defer unguarded.Close()

		session := connectMCP(t, unguarded.URL, readOnly)
		for tool, arguments := range map[string]map[string]any{
			"add_to_watchlist":      {"tmdb_id": 27205, "type": "movie"},
			"update_watchlist_item": {"id": matrix.ID.Hex(), "status": "finished"},
			"remove_from_watchlist": {"id": matrix.ID.Hex()},
		} {
			if message := refuse(t, session, tool, arguments); !strings.Contains(message, "watchlist:write") {
				t.Errorf("%s: message = %q, want it to name the missing scope", tool, message)
			}
		}
		unchanged(t)
	})

	t.Run("every tool that writes is guarded", func(t *testing.T) {
		_, scopes := h.api(nil).newMCPServer()
		want := map[string]string{
			"list_watchlist":        middleware.ScopeWatchlistRead,
			"whats_streaming":       middleware.ScopeWatchlistRead,
			"search_titles":         middleware.ScopeWatchlistRead,
			"add_to_watchlist":      middleware.ScopeWatchlistWrite,
			"update_watchlist_item": middleware.ScopeWatchlistWrite,
			"remove_from_watchlist": middleware.ScopeWatchlistWrite,
		}
		if !reflect.DeepEqual(scopes, want) {
			t.Errorf("scopes = %v, want %v", scopes, want)
		}
	})
}

func TestToolsCalled(t *testing.T) {
	tests := map[string][]string{
		callRequest("remove_from_watchlist", "{}"):                                       {"remove_from_watchlist"},
		"  \n" + callRequest("add_to_watchlist", "{}"):                                   {"add_to_watchlist"},
		"[" + listToolsRequest + ", " + callRequest("update_watchlist_item", "{}") + "]": {"update_watchlist_item"},
		listToolsRequest:                     nil,
		`{"method":"tools/call"}`:            {""},
		`{"method":"tools/call","params":7}`: nil,
		`not json`:                           nil,
		`[1, "two"]`:                         nil,
		``:                                   nil,
	}
	for body, want := range tests {
		if got := toolsCalled([]byte(body)); !slices.Equal(got, want) {
			t.Errorf("toolsCalled(%s) = %q, want %q", body, got, want)
		}
	}
}

// --- HTTP behaviour ---

func TestMCPAnswersOnlyPOST(t *testing.T) {
	h := newMCPHarness(t)
	valid := withToken(h.token(viewerEmail, readWrite))
	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut, http.MethodHead} {
		for name, edit := range map[string]func(*http.Request){"without a token": nil, "with a token": valid} {
			resp := h.request(method, mcpPath, "", edit)
			if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
				t.Errorf("%s %s: status = %d, Allow = %q; want 405 and POST", method, name, resp.StatusCode, resp.Header.Get("Allow"))
			}
			if resp.Header.Get("WWW-Authenticate") != "" {
				t.Errorf("%s %s: a 405 carried a challenge", method, name)
			}
		}
	}
}

func TestMCPOriginPolicy(t *testing.T) {
	h := newMCPHarness(t)
	valid := h.token(viewerEmail, readWrite)
	from := func(origin string, token string) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Set("Origin", origin)
			if token != "" {
				r.Header.Set("Authorization", "Bearer "+token)
			}
		}
	}

	t.Run("a page on another origin is refused, token or not", func(t *testing.T) {
		for _, origin := range []string{"https://evil.example", "null", "http://localhost:3000"} {
			for _, token := range []string{"", valid} {
				resp := h.post(listToolsRequest, from(origin, token))
				if resp.StatusCode != http.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
					t.Errorf("%s: status = %d, Allow-Origin = %q; want 403 and none",
						origin, resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
				}
			}
			preflight := h.request(http.MethodOptions, mcpPath, "", func(r *http.Request) {
				r.Header.Set("Origin", origin)
				r.Header.Set("Access-Control-Request-Method", "POST")
			})
			if preflight.StatusCode != http.StatusForbidden {
				t.Errorf("preflight from %s = %d, want 403", origin, preflight.StatusCode)
			}
		}
	})

	t.Run("the frontend and the API's own origin may call it, without credentials", func(t *testing.T) {
		for _, origin := range []string{frontendOrigin, h.baseURL} {
			resp := h.post(listToolsRequest, from(origin, valid))
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("%s: status = %d: %s", origin, resp.StatusCode, bodyOf(t, resp))
			}
			if resp.Header.Get("Access-Control-Allow-Origin") != origin || resp.Header.Get("Access-Control-Allow-Credentials") != "" {
				t.Errorf("%s: CORS headers = %v", origin, resp.Header)
			}

			// Without a token the page must be able to read the challenge.
			challenged := h.post(listToolsRequest, from(origin, ""))
			if challenged.StatusCode != http.StatusUnauthorized ||
				challenged.Header.Get("Access-Control-Expose-Headers") != "WWW-Authenticate" {
				t.Errorf("%s without a token: status = %d, headers = %v", origin, challenged.StatusCode, challenged.Header)
			}

			preflight := h.request(http.MethodOptions, mcpPath, "", func(r *http.Request) {
				r.Header.Set("Origin", origin)
				r.Header.Set("Access-Control-Request-Method", "POST")
				r.Header.Set("Access-Control-Request-Headers", "authorization, content-type, mcp-protocol-version")
			})
			allowed := preflight.Header.Get("Access-Control-Allow-Headers")
			if preflight.StatusCode != http.StatusNoContent || !strings.Contains(allowed, "Authorization") ||
				!strings.Contains(allowed, "MCP-Protocol-Version") || preflight.Header.Get("Access-Control-Allow-Credentials") != "" {
				t.Errorf("preflight from %s = %d, headers %v", origin, preflight.StatusCode, preflight.Header)
			}
		}
	})

	t.Run("a client that is not a browser sends no Origin and is served", func(t *testing.T) {
		resp := h.post(listToolsRequest, withToken(valid))
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("status = %d, Allow-Origin = %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
		}
	})

	// The REST API's policy is untouched: credentials, for the frontend only.
	t.Run("the REST API keeps its own policy", func(t *testing.T) {
		resp := h.request(http.MethodGet, "/health", "", func(r *http.Request) { r.Header.Set("Origin", frontendOrigin) })
		if resp.Header.Get("Access-Control-Allow-Origin") != frontendOrigin || resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
			t.Errorf("CORS headers of /health = %v", resp.Header)
		}
	})
}

func TestMCPResponsesAreOneUncompressedJSONDocument(t *testing.T) {
	h := newMCPHarness(t)
	h.seed()
	gzipped := func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+h.token(viewerEmail, readWrite))
		r.Header.Set("Accept-Encoding", "gzip")
	}

	resp := h.post(callRequest("list_watchlist", "{}"), gzipped)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, bodyOf(t, resp))
	}
	if resp.Header.Get("Content-Encoding") != "" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		t.Errorf("Content-Encoding = %q, Content-Type = %q; want none and JSON",
			resp.Header.Get("Content-Encoding"), resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("a stateless endpoint handed out a session: %q", resp.Header.Get("Mcp-Session-Id"))
	}
	var message struct {
		Result struct {
			StructuredContent listWatchlistOutput `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(bodyOf(t, resp)), &message); err != nil || message.Result.StructuredContent.Total != 3 {
		t.Errorf("body did not decode as one JSON-RPC result with the list: %v, %+v", err, message)
	}

	// The same request to the REST API is compressed: the middleware is on,
	// just not for the MCP routes.
	rest := h.request(http.MethodGet, "/health", "", func(r *http.Request) { r.Header.Set("Accept-Encoding", "gzip") })
	if rest.Header.Get("Content-Encoding") != "gzip" {
		t.Errorf("Content-Encoding of /health = %q, want gzip", rest.Header.Get("Content-Encoding"))
	}
}

func TestMCPLimitsTheRequestBody(t *testing.T) {
	h := newMCPHarness(t)
	padding := strings.Repeat(" ", mcpMaxBodyBytes)
	resp := h.post(listToolsRequest+padding, withToken(h.token(viewerEmail, readWrite)))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
	// Refused before it is read when the caller is not authorized at all.
	if resp := h.post(listToolsRequest+padding, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a token: status = %d, want 401", resp.StatusCode)
	}
}

func TestMCPQueryStringsNeverReachTheLog(t *testing.T) {
	var seen []string
	handler := withoutMCPQuery(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Path+"?"+r.URL.RawQuery+" "+r.RequestURI)
	}))
	for _, target := range []string{"/mcp?access_token=secret", mcpMetadataPath + "/mcp?access_token=secret", "/api/search?query=dune"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, target, nil))
	}
	want := []string{"/mcp? /mcp", mcpMetadataPath + "/mcp? " + mcpMetadataPath + "/mcp", "/api/search?query=dune /api/search?query=dune"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("requests seen = %q, want %q", seen, want)
	}
}

func TestMCPKeepsNoStateBetweenRequests(t *testing.T) {
	h := newMCPHarness(t)
	h.seed()
	// A tool call with no initialize before it and no session, as any
	// instance behind a load balancer might receive it.
	resp := h.post(callRequest("list_watchlist", `{"limit":1}`), withToken(h.token(viewerEmail, readScope)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.StatusCode, bodyOf(t, resp))
	}
	// And the next request is checked again: the same connection with a
	// token that has since expired gets nothing.
	expired := h.issuer.Claims(h.resource, viewerEmail, readWrite)
	expired["exp"] = time.Now().Add(-10 * time.Minute).Unix()
	if resp := h.post(callRequest("list_watchlist", "{}"), withToken(h.issuer.Sign(expired))); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an expired token: status = %d, want 401", resp.StatusCode)
	}
}

// --- the tools, through the SDK's client ---

func TestMCPAdvertisesItsTools(t *testing.T) {
	h := newMCPHarness(t)
	session := h.connect(viewerEmail, readScope)

	init := session.InitializeResult()
	if init.ServerInfo.Name != mcpServerName || init.Instructions == "" {
		t.Errorf("server info = %+v, instructions = %q", init.ServerInfo, init.Instructions)
	}
	if init.Capabilities.Tools == nil || init.Capabilities.Tools.ListChanged || init.Capabilities.Logging != nil ||
		init.Capabilities.Resources != nil || init.Capabilities.Prompts != nil {
		t.Errorf("capabilities = %+v, want tools only, without change notifications", init.Capabilities)
	}

	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tools := map[string]*mcp.Tool{}
	var names []string
	for _, tool := range listed.Tools {
		tools[tool.Name] = tool
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"add_to_watchlist", "list_watchlist", "remove_from_watchlist", "search_titles", "update_watchlist_item", "whats_streaming"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}

	for name, tool := range tools {
		if tool.Description == "" || tool.Title == "" || tool.Annotations == nil || tool.OutputSchema == nil {
			t.Errorf("%s: description, title, annotations and output schema are all expected: %+v", name, tool)
			continue
		}
		schema := schemaJSON(t, tool.InputSchema)
		for property, raw := range schema.Properties {
			if raw.Description == "" {
				t.Errorf("%s: input %q has no description", name, property)
			}
		}
		readOnly := name == "list_watchlist" || name == "whats_streaming" || name == "search_titles"
		if tool.Annotations.ReadOnlyHint != readOnly {
			t.Errorf("%s: readOnlyHint = %v", name, tool.Annotations.ReadOnlyHint)
		}
		if !tool.Annotations.IdempotentHint {
			t.Errorf("%s: idempotentHint = false", name)
		}
		destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
		if !readOnly && destructive != (name == "remove_from_watchlist") {
			t.Errorf("%s: destructiveHint = %v", name, destructive)
		}
	}

	list := schemaJSON(t, tools["list_watchlist"].InputSchema)
	if got := list.Properties["limit"]; string(got.Default) != "25" || got.Maximum != 100 || got.Minimum != 1 {
		t.Errorf("list_watchlist limit = %+v, want default 25 within 1..100", got)
	}
	if got := list.Properties["status"].Enum; !reflect.DeepEqual(got, []string{"planned", "watching", "finished"}) {
		t.Errorf("list_watchlist status enum = %v", got)
	}
	if len(list.Required) != 0 {
		t.Errorf("list_watchlist requires %v, want nothing", list.Required)
	}
	add := schemaJSON(t, tools["add_to_watchlist"].InputSchema)
	sort.Strings(add.Required)
	if !reflect.DeepEqual(add.Required, []string{"tmdb_id", "type"}) || !reflect.DeepEqual(add.Properties["type"].Enum, []string{"movie", "show"}) {
		t.Errorf("add_to_watchlist schema = %+v", add)
	}
	update := schemaJSON(t, tools["update_watchlist_item"].InputSchema)
	if !reflect.DeepEqual(update.Required, []string{"id"}) || string(update.Properties["tags"].Type) != `"array"` {
		t.Errorf("update_watchlist_item schema = %+v (tags type %s)", update, update.Properties["tags"].Type)
	}
}

type schemaView struct {
	Required   []string `json:"required"`
	Properties map[string]struct {
		Type        json.RawMessage `json:"type"`
		Description string          `json:"description"`
		Default     json.RawMessage `json:"default"`
		Minimum     float64         `json:"minimum"`
		Maximum     float64         `json:"maximum"`
		Enum        []string        `json:"enum"`
	} `json:"properties"`
}

func schemaJSON(t *testing.T, schema any) schemaView {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var view schemaView
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatalf("schema %s: %v", raw, err)
	}
	return view
}

func TestMCPListWatchlist(t *testing.T) {
	h := newMCPHarness(t)
	matrix, _, _, _ := h.seed()
	session := h.connect(viewerEmail, readScope)

	t.Run("everything, newest first, with where it streams", func(t *testing.T) {
		out, result := succeed[listWatchlistOutput](t, session, "list_watchlist", nil)
		want := listWatchlistOutput{
			Items: []mcpItem{
				{ID: matrix.ID.Hex(), Title: "The Matrix", Year: 1999, Type: "movie", Status: "planned",
					Tags: []string{"sci-fi"}, TMDbID: 603, StreamingOn: []string{"Netflix"}},
				{ID: out.Items[1].ID, Title: "Game of Thrones", Year: 2011, Type: "show", Status: "watching",
					Tags: []string{}, TMDbID: 1399, StreamingOn: []string{}},
				{ID: out.Items[2].ID, Title: "Arrival", Year: 2016, Type: "movie", Status: "finished",
					Tags: []string{}, TMDbID: 329865, StreamingOn: []string{"Hulu", "Netflix"}},
			},
			Total: 3, Region: "US",
		}
		if !reflect.DeepEqual(out, want) {
			t.Errorf("list =\n%+v\nwant\n%+v", out, want)
		}

		// A summary to read, then the same data as text for clients that do
		// not take structured content.
		if got := textOf(result, 0); got != "3 of 3 matching watchlist items." {
			t.Errorf("summary = %q", got)
		}
		var fromText listWatchlistOutput
		if err := json.Unmarshal([]byte(textOf(result, 1)), &fromText); err != nil || !reflect.DeepEqual(fromText, out) {
			t.Errorf("text content = %s (%v), want the structured content as JSON", textOf(result, 1), err)
		}
		// Small on purpose: nothing a model does not need to talk about the list.
		for _, unwanted := range []string{"poster", "overview", "synopsis", "user_id", "added_at", "logo", "Heat", "theirs"} {
			if strings.Contains(textOf(result, 1), unwanted) {
				t.Errorf("the result contains %q: %s", unwanted, textOf(result, 1))
			}
		}
	})

	t.Run("filters", func(t *testing.T) {
		tests := []struct {
			arguments map[string]any
			want      []string
		}{
			{map[string]any{"status": "watching"}, []string{"Game of Thrones"}},
			{map[string]any{"type": "movie"}, []string{"The Matrix", "Arrival"}},
			{map[string]any{"query": "ARR"}, []string{"Arrival"}},
			{map[string]any{"streaming_only": true}, []string{"The Matrix", "Arrival"}},
			{map[string]any{"streaming_only": true, "status": "planned"}, []string{"The Matrix"}},
			{map[string]any{"query": "nothing like this"}, []string{}},
		}
		for _, tt := range tests {
			out, _ := succeed[listWatchlistOutput](t, session, "list_watchlist", tt.arguments)
			if got := titlesOf(out.Items); !reflect.DeepEqual(got, tt.want) || out.Total != len(tt.want) {
				t.Errorf("%v: titles = %v (total %d), want %v", tt.arguments, got, out.Total, tt.want)
			}
		}
	})

	t.Run("paging", func(t *testing.T) {
		var seen []string
		arguments := map[string]any{"limit": 2}
		for page := 0; page < 5; page++ {
			out, result := succeed[listWatchlistOutput](t, session, "list_watchlist", arguments)
			seen = append(seen, titlesOf(out.Items)...)
			if out.Total != 3 {
				t.Fatalf("total = %d, want 3", out.Total)
			}
			if out.NextOffset == 0 {
				break
			}
			if page == 0 && textOf(result, 0) != "2 of 3 matching watchlist items. More from offset 2." {
				t.Errorf("summary = %q", textOf(result, 0))
			}
			arguments["offset"] = out.NextOffset
		}
		if !reflect.DeepEqual(seen, []string{"The Matrix", "Game of Thrones", "Arrival"}) {
			t.Errorf("pages = %v", seen)
		}
	})

	t.Run("arguments outside the schema are refused before the tool runs", func(t *testing.T) {
		for _, arguments := range []map[string]any{
			{"status": "abandoned"},
			{"type": "tv"},
			{"limit": 101},
			{"limit": 0},
			{"offset": -1},
			{"streaming_only": "yes"},
			{"sort": "title"},
		} {
			if message := refuse(t, session, "list_watchlist", arguments); message == "" {
				t.Errorf("%v: refused without a message", arguments)
			}
		}
	})

	t.Run("a database failure is reported without its details", func(t *testing.T) {
		h.store.Fail["ListWatchlistItems"] = fmt.Errorf("mongo: connection reset by 10.0.0.7:27017")
		defer delete(h.store.Fail, "ListWatchlistItems")
		if message := refuse(t, session, "list_watchlist", nil); strings.Contains(message, "mongo") || strings.Contains(message, "10.0.0.7") {
			t.Errorf("message = %q", message)
		}
	})
}

func TestMCPWhatsStreaming(t *testing.T) {
	h := newMCPHarness(t)
	matrix, _, arrival, _ := h.seed()
	session := h.connect(viewerEmail, readScope)

	out, result := succeed[whatsStreamingOutput](t, session, "whats_streaming", nil)
	want := whatsStreamingOutput{
		Services: []streamingService{
			{Service: "Hulu", Titles: []streamingTitle{
				{ID: arrival.ID.Hex(), Title: "Arrival", Year: 2016, Type: "movie", Status: "finished"},
			}},
			{Service: "Netflix", Titles: []streamingTitle{
				{ID: arrival.ID.Hex(), Title: "Arrival", Year: 2016, Type: "movie", Status: "finished"},
				{ID: matrix.ID.Hex(), Title: "The Matrix", Year: 1999, Type: "movie", Status: "planned"},
			}},
		},
		Streaming: 2, NotStreaming: 1, YourServices: []string{"Hulu", "Netflix"}, Region: "US",
	}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("whats_streaming =\n%+v\nwant\n%+v", out, want)
	}
	if got := textOf(result, 0); got != "2 watchlist titles are streaming on the user's services (Hulu: 1, Netflix: 2); 1 are not." {
		t.Errorf("summary = %q", got)
	}
	if h.titles.calls != 0 {
		t.Errorf("answering from stored availability asked TMDb %d times", h.titles.calls)
	}

	shows, result := succeed[whatsStreamingOutput](t, session, "whats_streaming", map[string]any{"type": "show"})
	if len(shows.Services) != 0 || shows.Streaming != 0 || shows.NotStreaming != 1 {
		t.Errorf("shows = %+v, want none streaming and one not", shows)
	}
	if !strings.Contains(textOf(result, 0), "Nothing on the watchlist is streaming") {
		t.Errorf("summary = %q", textOf(result, 0))
	}

	// Someone who has not picked their services is told so, instead of
	// being told nothing is on.
	none, result := succeed[whatsStreamingOutput](t, h.connect(otherEmail, readScope), "whats_streaming", nil)
	if len(none.Services) != 0 || len(none.YourServices) != 0 || none.NotStreaming != 1 {
		t.Errorf("without services = %+v", none)
	}
	if !strings.Contains(textOf(result, 0), "has not chosen any streaming services") {
		t.Errorf("summary = %q", textOf(result, 0))
	}
}

func TestMCPSearchTitles(t *testing.T) {
	h := newMCPHarness(t)
	long := strings.Repeat("A very long synopsis. ", 30)
	h.search.finds("movie", fmt.Sprintf(`{"page":1,"total_pages":1,"results":[
		{"id":438631,"title":"Dune","release_date":"2021-09-15","poster_path":"/dune.jpg","overview":"Paul Atreides leads\n  a rebellion."},
		{"id":841,"title":"Dune","release_date":"1984-12-14","overview":%q},
		{"id":693134,"title":"Dune: Part Two","release_date":"2024-02-27"}
	]}`, long))
	h.search.finds("tv", `{"page":1,"total_pages":1,"results":[
		{"id":90228,"name":"Dune: Prophecy","first_air_date":"2024-11-17","overview":"Two Harkonnen sisters."}
	]}`)
	session := h.connect(viewerEmail, readScope)

	out, result := succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": " Dune "})
	want := []searchResult{
		{TMDbID: 438631, Type: "movie", Title: "Dune", Year: 2021, Overview: "Paul Atreides leads a rebellion."},
		{TMDbID: 90228, Type: "show", Title: "Dune: Prophecy", Year: 2024, Overview: "Two Harkonnen sisters."},
		{TMDbID: 841, Type: "movie", Title: "Dune", Year: 1984, Overview: out.Results[2].Overview},
		{TMDbID: 693134, Type: "movie", Title: "Dune: Part Two", Year: 2024},
	}
	if !reflect.DeepEqual(out.Results, want) {
		t.Errorf("results =\n%+v\nwant\n%+v", out.Results, want)
	}
	if cut := out.Results[2].Overview; len([]rune(cut)) > mcpOverviewLen || !strings.HasSuffix(cut, "…") || strings.Contains(cut, "\n") {
		t.Errorf("a long overview came back as %q", cut)
	}
	if got := textOf(result, 0); got != `4 titles found for "Dune".` {
		t.Errorf("summary = %q", got)
	}
	if strings.Contains(textOf(result, 1), "dune.jpg") {
		t.Errorf("the result carries posters: %s", textOf(result, 1))
	}
	if h.search.requests() != 2 {
		t.Fatalf("TMDb was searched %d times, want once for movies and once for shows", h.search.requests())
	}

	t.Run("a repeated search is answered from the cache", func(t *testing.T) {
		again, _ := succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": "dune"})
		if !reflect.DeepEqual(again.Results, want) || h.search.requests() != 2 {
			t.Errorf("results changed or TMDb was asked again (%d requests)", h.search.requests())
		}
	})

	t.Run("the cache is the one GET /api/search fills", func(t *testing.T) {
		rest := h.request(http.MethodGet, "/api/search?query=arrival&type=movie", "",
			withToken(h.issuer.Token(h.restAud, viewerEmail, "")))
		if rest.StatusCode != http.StatusOK {
			t.Fatalf("GET /api/search = %d: %s", rest.StatusCode, bodyOf(t, rest))
		}
		before := h.search.requests("movie")
		succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": "Arrival", "type": "movie"})
		if h.search.requests("movie") != before {
			t.Errorf("the tool searched TMDb for what the REST API had just cached")
		}
	})

	t.Run("one type, and a limit", func(t *testing.T) {
		shows, _ := succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": "dune", "type": "show"})
		if len(shows.Results) != 1 || shows.Results[0].Type != "show" {
			t.Errorf("shows = %+v", shows.Results)
		}
		two, _ := succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": "dune", "limit": 2})
		if len(two.Results) != 2 || two.Results[1].Type != "show" {
			t.Errorf("limit 2 = %+v, want the best movie and the best show", two.Results)
		}
	})

	t.Run("nothing found is an answer, not an error", func(t *testing.T) {
		h.search.finds("movie", "")
		h.search.finds("tv", "")
		none, result := succeed[searchTitlesOutput](t, session, "search_titles", map[string]any{"query": "zzzz"})
		if none.Results == nil || len(none.Results) != 0 || textOf(result, 0) != `No titles found for "zzzz".` {
			t.Errorf("results = %#v, summary = %q", none.Results, textOf(result, 0))
		}
	})

	t.Run("bad arguments", func(t *testing.T) {
		for _, arguments := range []map[string]any{{}, {"query": ""}, {"query": "   "}, {"query": "x", "type": "tv"}, {"query": "x", "limit": 21}} {
			refuse(t, session, "search_titles", arguments)
		}
	})

	t.Run("a TMDb failure is a tool error", func(t *testing.T) {
		h.search.fails(http.StatusUnauthorized)
		defer h.search.fails(0)
		if message := refuse(t, session, "search_titles", map[string]any{"query": "never searched before"}); !strings.Contains(message, "search failed") {
			t.Errorf("message = %q", message)
		}
	})
}

func TestMCPAddToWatchlist(t *testing.T) {
	h := newMCPHarness(t)
	h.seed()
	h.titles.titles = map[string]*tmdb.TitleDetails{
		"movie:27205": {
			ID: 27205, MediaType: "movie", Title: "Inception", Year: 2010,
			Providers: map[string]tmdb.RegionProviders{"US": {Flatrate: []tmdb.Provider{{ProviderID: 15, ProviderName: "Hulu"}}}},
		},
	}
	session := h.connect(viewerEmail, readWrite)
	inception := map[string]any{"tmdb_id": 27205, "type": "movie"}

	added, result := succeed[addToWatchlistOutput](t, session, "add_to_watchlist", inception)
	want := mcpItem{ID: added.Item.ID, Title: "Inception", Year: 2010, Type: "movie", Status: "planned",
		Tags: []string{}, TMDbID: 27205, StreamingOn: []string{"Hulu"}}
	if added.AlreadyOnWatchlist || !reflect.DeepEqual(added.Item, want) {
		t.Fatalf("added = %+v, want %+v", added, want)
	}
	if got := textOf(result, 0); got != "Added Inception (2010) to the watchlist. It is streaming on Hulu." {
		t.Errorf("summary = %q", got)
	}
	stored := h.store.Items(h.viewer.ID)
	if len(stored) != 4 || stored[3].ID.Hex() != added.Item.ID || stored[3].UserID != h.viewer.ID {
		t.Fatalf("stored items = %d, last = %+v", len(stored), stored[len(stored)-1])
	}

	t.Run("adding it again changes nothing and says so", func(t *testing.T) {
		writes, lookups := h.store.Writes, h.titles.calls
		for range 3 {
			again, result := succeed[addToWatchlistOutput](t, session, "add_to_watchlist", inception)
			if !again.AlreadyOnWatchlist || !reflect.DeepEqual(again.Item, want) {
				t.Errorf("again = %+v, want the same item, already on the watchlist", again)
			}
			if !strings.Contains(textOf(result, 0), "already on the watchlist") {
				t.Errorf("summary = %q", textOf(result, 0))
			}
		}
		if len(h.store.Items(h.viewer.ID)) != 4 || h.store.Writes != writes || h.titles.calls != lookups {
			t.Errorf("a repeated add wrote to the store or asked TMDb again")
		}
	})

	t.Run("a title someone else has is still new to this user", func(t *testing.T) {
		// Heat is on the other user's list, and its title is already stored.
		lookups := h.titles.calls
		heat, _ := succeed[addToWatchlistOutput](t, session, "add_to_watchlist", map[string]any{"tmdb_id": 949, "type": "movie"})
		if heat.AlreadyOnWatchlist || heat.Item.Title != "Heat" || heat.Item.Year != 1995 ||
			!reflect.DeepEqual(heat.Item.StreamingOn, []string{"Netflix"}) {
			t.Errorf("added = %+v", heat)
		}
		if h.titles.calls != lookups {
			t.Errorf("a stored title was fetched from TMDb again")
		}
		if got := h.store.Items(h.other.ID); len(got) != 1 || !reflect.DeepEqual(got[0].Tags, []string{"theirs"}) {
			t.Errorf("the other user's list changed: %+v", got)
		}
	})

	t.Run("the same id as a show is a different title", func(t *testing.T) {
		message := refuse(t, session, "add_to_watchlist", map[string]any{"tmdb_id": 27205, "type": "show"})
		if !strings.Contains(message, "TMDb has no show with id 27205") {
			t.Errorf("message = %q", message)
		}
	})

	t.Run("nothing is added when the title cannot be resolved", func(t *testing.T) {
		before := len(h.store.Items(h.viewer.ID))
		if message := refuse(t, session, "add_to_watchlist", map[string]any{"tmdb_id": 999999, "type": "movie"}); !strings.Contains(message, "search_titles") {
			t.Errorf("unknown id: message = %q", message)
		}
		h.titles.err = fmt.Errorf("tmdb title: connection refused")
		defer func() { h.titles.err = nil }()
		if message := refuse(t, session, "add_to_watchlist", map[string]any{"tmdb_id": 155, "type": "movie"}); !strings.Contains(message, "nothing was added") || strings.Contains(message, "connection refused") {
			t.Errorf("TMDb down: message = %q", message)
		}
		for _, arguments := range []map[string]any{
			{"tmdb_id": 0, "type": "movie"}, {"tmdb_id": -5, "type": "movie"}, {"tmdb_id": 155},
			{"type": "movie"}, {"tmdb_id": 155, "type": "tv"}, {"tmdb_id": "155", "type": "movie"},
		} {
			refuse(t, session, "add_to_watchlist", arguments)
		}
		if after := len(h.store.Items(h.viewer.ID)); after != before {
			t.Errorf("items = %d, was %d", after, before)
		}
	})
}

func TestMCPAddToWatchlistCreatesTheAccountOnFirstUse(t *testing.T) {
	h := newMCPHarness(t)
	h.titles.streamsOn("movie", 603, tmdb.Provider{ProviderID: 8, ProviderName: "Netflix"})
	session := h.connect("new@example.com", readWrite)

	empty, _ := succeed[listWatchlistOutput](t, session, "list_watchlist", nil)
	if empty.Total != 0 || empty.Items == nil {
		t.Errorf("a new user's list = %+v, want an empty list", empty)
	}
	added, _ := succeed[addToWatchlistOutput](t, session, "add_to_watchlist", map[string]any{"tmdb_id": 603, "type": "movie"})

	user, err := h.store.GetUserByEmail(context.Background(), "new@example.com")
	if err != nil || user == nil {
		t.Fatalf("the account was not created: %v, %v", user, err)
	}
	// The same account the REST API would have made: name and picture from
	// the token, the default region, no services yet.
	if user.Name != "Test User" || user.Region != "US" || len(user.Services) != 0 || h.store.Queries["UpsertUser"] != 1 {
		t.Errorf("user = %+v, created %d times", user, h.store.Queries["UpsertUser"])
	}
	if items := h.store.Items(user.ID); len(items) != 1 || items[0].ID.Hex() != added.Item.ID {
		t.Errorf("the new user's items = %+v", items)
	}
	// Not streaming for them: they have picked no services.
	if len(added.Item.StreamingOn) != 0 {
		t.Errorf("streaming_on = %v for a user without services", added.Item.StreamingOn)
	}
}

func TestMCPUpdateWatchlistItem(t *testing.T) {
	h := newMCPHarness(t)
	matrix, thrones, _, _ := h.seed()
	session := h.connect(viewerEmail, readWrite)
	id := matrix.ID.Hex()
	stored := func() *models.WatchlistItem {
		for _, item := range h.store.Items(h.viewer.ID) {
			if item.ID == matrix.ID {
				return item
			}
		}
		t.Fatal("the item is gone")
		return nil
	}

	out, result := succeed[updateWatchlistItemOutput](t, session, "update_watchlist_item", map[string]any{"id": id, "status": "watching"})
	want := mcpItem{ID: id, Title: "The Matrix", Year: 1999, Type: "movie", Status: "watching",
		Tags: []string{"sci-fi"}, TMDbID: 603, StreamingOn: []string{"Netflix"}}
	if !reflect.DeepEqual(out.Item, want) {
		t.Errorf("after a status change = %+v, want %+v (tags untouched)", out.Item, want)
	}
	if got := textOf(result, 0); got != "Updated The Matrix (1999): status watching." {
		t.Errorf("summary = %q", got)
	}
	if got := stored(); got.Status != "watching" || !reflect.DeepEqual(got.Tags, []string{"sci-fi"}) {
		t.Errorf("stored = %+v", got)
	}

	out, _ = succeed[updateWatchlistItemOutput](t, session, "update_watchlist_item",
		map[string]any{"id": id, "tags": []string{" rewatch ", "classic", "rewatch", ""}})
	if !reflect.DeepEqual(out.Item.Tags, []string{"rewatch", "classic"}) || out.Item.Status != "watching" {
		t.Errorf("after new tags = %+v, want them cleaned and the status untouched", out.Item)
	}

	out, result = succeed[updateWatchlistItemOutput](t, session, "update_watchlist_item",
		map[string]any{"id": id, "status": "finished", "tags": []string{}})
	if len(out.Item.Tags) != 0 || out.Item.Status != "finished" || len(stored().Tags) != 0 {
		t.Errorf("after clearing tags = %+v, stored %+v", out.Item, stored())
	}
	if got := textOf(result, 0); got != "Updated The Matrix (1999): status finished; no tags." {
		t.Errorf("summary = %q", got)
	}

	t.Run("bad requests change nothing", func(t *testing.T) {
		writes := h.store.Writes
		for name, arguments := range map[string]map[string]any{
			"nothing to change":    {"id": id},
			"an unknown status":    {"id": id, "status": "dropped"},
			"not an id":            {"id": "the-matrix", "status": "planned"},
			"no id":                {"status": "planned"},
			"an id that is no one": {"id": primitive.NewObjectID().Hex(), "status": "planned"},
			"null tags":            {"id": id, "tags": nil},
			"too many tags":        {"id": id, "tags": strings.Split(strings.Repeat("t,", 21), ",")},
			"a title":              {"id": id, "title": "Renamed"},
		} {
			if message := refuse(t, session, "update_watchlist_item", arguments); message == "" {
				t.Errorf("%s: refused without a message", name)
			}
		}
		if h.store.Writes != writes {
			t.Errorf("a refused update wrote to the store")
		}
		_ = thrones
	})
}

func TestMCPRemoveFromWatchlist(t *testing.T) {
	h := newMCPHarness(t)
	matrix, _, _, _ := h.seed()
	session := h.connect(viewerEmail, readWrite)

	out, _ := succeed[removeFromWatchlistOutput](t, session, "remove_from_watchlist", map[string]any{"id": matrix.ID.Hex()})
	if !out.Removed || out.ID != matrix.ID.Hex() {
		t.Errorf("result = %+v", out)
	}
	if got := titlesOf(listAll(t, session)); !reflect.DeepEqual(got, []string{"Game of Thrones", "Arrival"}) {
		t.Errorf("the list afterwards = %v", got)
	}

	// Gone is gone: a second call has nothing to remove and removes nothing.
	if message := refuse(t, session, "remove_from_watchlist", map[string]any{"id": matrix.ID.Hex()}); !strings.Contains(message, "No item with that id") {
		t.Errorf("second removal: message = %q", message)
	}
	refuse(t, session, "remove_from_watchlist", map[string]any{"id": "603"})
	refuse(t, session, "remove_from_watchlist", nil)
	if len(h.store.Items(h.viewer.ID)) != 2 {
		t.Errorf("items = %d, want 2", len(h.store.Items(h.viewer.ID)))
	}
}

func listAll(t *testing.T, session *mcp.ClientSession) []mcpItem {
	t.Helper()
	out, _ := succeed[listWatchlistOutput](t, session, "list_watchlist", map[string]any{"limit": 100})
	return out.Items
}

// The token decides whose watchlist a call touches. Knowing another user's
// item ID gets a caller nothing: not a read, not a change, and not even
// confirmation that the ID exists.
func TestMCPUsersCannotReachEachOthersItems(t *testing.T) {
	h := newMCPHarness(t)
	matrix, _, _, heat := h.seed()
	viewer := h.connect(viewerEmail, readWrite)
	other := h.connect(otherEmail, readWrite)

	if got := titlesOf(listAll(t, other)); !reflect.DeepEqual(got, []string{"Heat"}) {
		t.Errorf("the other user's list = %v", got)
	}
	if got := titlesOf(listAll(t, viewer)); slices.Contains(got, "Heat") || len(got) != 3 {
		t.Errorf("the viewer's list = %v", got)
	}
	streaming, _ := succeed[whatsStreamingOutput](t, viewer, "whats_streaming", nil)
	if raw, _ := json.Marshal(streaming); bytes.Contains(raw, []byte("Heat")) || bytes.Contains(raw, []byte(heat.ID.Hex())) {
		t.Errorf("whats_streaming shows another user's title: %s", raw)
	}

	missing := refuse(t, viewer, "update_watchlist_item", map[string]any{"id": primitive.NewObjectID().Hex(), "status": "finished"})
	writes := h.store.Writes
	for _, attempt := range []struct {
		session *mcp.ClientSession
		target  *models.WatchlistItem
	}{{viewer, heat}, {other, matrix}} {
		id := attempt.target.ID.Hex()
		if message := refuse(t, attempt.session, "update_watchlist_item", map[string]any{"id": id, "status": "finished", "tags": []string{"mine"}}); message != missing {
			t.Errorf("update of another user's item: %q, want the answer for an id that does not exist (%q)", message, missing)
		}
		if message := refuse(t, attempt.session, "remove_from_watchlist", map[string]any{"id": id}); message != missing {
			t.Errorf("removal of another user's item: %q, want %q", message, missing)
		}
	}
	if h.store.Writes != writes {
		t.Errorf("an attempt on another user's item wrote to the store")
	}

	theirs := h.store.Items(h.other.ID)
	if len(theirs) != 1 || theirs[0].Status != models.WatchlistStatusPlanned || !reflect.DeepEqual(theirs[0].Tags, []string{"theirs"}) {
		t.Errorf("the other user's item changed: %+v", theirs)
	}
	mine := h.store.Items(h.viewer.ID)
	if len(mine) != 3 || mine[0].Status != models.WatchlistStatusPlanned || !reflect.DeepEqual(mine[0].Tags, []string{"sci-fi"}) {
		t.Errorf("the viewer's items changed: %+v", mine)
	}

	// Adding a title the other user has makes a second item, on the caller's
	// own list, and leaves theirs alone.
	h.titles.streamsOn("movie", 603)
	added, _ := succeed[addToWatchlistOutput](t, other, "add_to_watchlist", map[string]any{"tmdb_id": 603, "type": "movie"})
	if added.AlreadyOnWatchlist || added.Item.ID == matrix.ID.Hex() {
		t.Errorf("the other user was handed the viewer's item: %+v", added)
	}
	if got := titlesOf(listAll(t, other)); len(got) != 2 {
		t.Errorf("the other user's list = %v", got)
	}
}
