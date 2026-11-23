package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/claude"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"github.com/neilsmahajan/watchlist-notify/internal/tonight"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

const tonightPath = "/api/recommendations/tonight"

// pickerModel stands in for Claude behind the endpoint.
type pickerModel struct {
	mu     sync.Mutex
	calls  int
	prompt string // the last user message
	answer string
	err    error
}

func (m *pickerModel) Model() string { return "claude-test-model" }

func (m *pickerModel) CompleteJSON(_ context.Context, _, user string, _ map[string]any) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.prompt = user
	if m.err != nil {
		return "", m.err
	}
	if m.answer != "" {
		return m.answer, nil
	}
	return `{"picks":[{"index":1,"reason":"A reason for the first."},{"index":2,"reason":"A reason for the second."},{"index":3,"reason":"A reason for the third."}]}`, nil
}

func (m *pickerModel) called() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// tonightHarness is the API as RegisterRoutes builds it, with an in-memory
// store, a fake model and a stand-in for the JWT middleware that signs every
// request in as email.
type tonightHarness struct {
	t      *testing.T
	store  *storetest.Store
	model  *pickerModel
	user   *models.User
	email  string // who the requests are signed in as
	router http.Handler
	nextID int
}

// newTonightHarness builds the API. A nil limits leaves the feature off, as
// on a server without an Anthropic key.
func newTonightHarness(t *testing.T, limits *tonight.Limits) *tonightHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)

	store := storetest.New()
	h := &tonightHarness{t: t, store: store, model: &pickerModel{}, email: viewerEmail, nextID: 500}
	h.user = store.AddUser(&models.User{
		Email:  viewerEmail,
		Name:   "Sam Viewer",
		Region: "US",
		Services: []models.ServiceSubscription{
			{Code: models.ServiceNetflix, Active: true},
			{Code: models.ServiceHulu, Active: true},
		},
	})

	lister := watchlist.NewLister(store, nil, "US")
	s := &Server{db: store, lister: lister, defaultRegion: "US"}
	if limits != nil {
		s.recommender = tonight.New(lister, h.model, store, *limits)
	}
	router, err := s.routes(routeConfig{
		origins:  []string{"http://localhost:3000"},
		restAuth: func(c *gin.Context) { c.Set("user_email", h.email) },
	})
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	h.router = router
	return h
}

var defaultLimits = &tonight.Limits{PerUser: tonight.DefaultPerUser, Total: tonight.DefaultTotal}

// addTitles puts n films on the viewer's list, all streaming on Netflix.
func (h *tonightHarness) addTitles(n int) []*models.WatchlistItem {
	h.t.Helper()
	items := make([]*models.WatchlistItem, n)
	for i := range items {
		h.nextID++
		items[i] = h.store.AddItem(&models.WatchlistItem{
			UserID: h.user.ID, Title: fmt.Sprintf("Film %02d", i+1), Type: models.WatchlistTypeMovie,
			Status: models.WatchlistStatusPlanned, Year: 2021, TMDbID: h.nextID,
			AddedAt: base.Add(-time.Duration(h.nextID) * time.Minute), UpdatedAt: base,
		})
		h.store.PutTitle(&models.Title{
			ID:            fmt.Sprintf("movie:%d", h.nextID),
			TitleMetadata: models.TitleMetadata{PosterPath: "/poster.jpg", Rating: 7.5, Runtime: 100, Genres: []string{"Drama"}, Overview: "An overview."},
			Providers: map[string]models.RegionSnapshot{"US": {
				Link: "https://example.test/watch", FetchedAt: base,
				Offers: []models.ProviderOffer{offer(8, "Netflix", models.AccessSubscription)},
			}},
		})
	}
	return items
}

func (h *tonightHarness) do(method, target, body string) *httptest.ResponseRecorder {
	h.t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, req)
	return rec
}

// ask posts to the endpoint and decodes the JSON object it answers with.
func (h *tonightHarness) ask(body string, wantStatus int) map[string]any {
	h.t.Helper()
	return h.decode(h.do(http.MethodPost, tonightPath, body), wantStatus)
}

func (h *tonightHarness) decode(rec *httptest.ResponseRecorder, wantStatus int) map[string]any {
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

func pickedTitles(body map[string]any) []string {
	names := []string{}
	for _, raw := range body["picks"].([]any) {
		names = append(names, raw.(map[string]any)["item"].(map[string]any)["title"].(string))
	}
	return names
}

// promptTitles reads the numbered titles out of the message sent to the model:
// one JSON object per line.
func promptTitles(t *testing.T, prompt string) map[int]string {
	t.Helper()
	numbered := map[int]string{}
	for _, line := range strings.Split(prompt, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var fact struct {
			I     int    `json:"i"`
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(line), &fact); err != nil {
			t.Fatalf("a title line is not JSON: %v", err)
		}
		numbered[fact.I] = fact.Title
	}
	return numbered
}

// captureLog collects everything the standard logger writes during the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return &buf
}

// --- the feature switch ---

func TestTonightIsOffWithoutAKey(t *testing.T) {
	h := newTonightHarness(t, nil)
	h.addTitles(6)

	body := h.ask(`{"mood":"something light"}`, http.StatusServiceUnavailable)
	if msg, _ := body["error"].(string); msg == "" || len(body) != 1 {
		t.Errorf("body = %v, want only an error message", body)
	}
	if h.model.called() != 0 || len(h.store.UsageKeys()) != 0 {
		t.Error("a switched-off feature called the model or counted usage")
	}

	// The profile says so, which is what the frontend goes by.
	me := h.decode(h.do(http.MethodGet, "/api/me", ""), http.StatusOK)
	if got := me["features"]; !reflect.DeepEqual(got, map[string]any{"recommendations": false}) {
		t.Errorf("features = %v, want recommendations off", got)
	}
}

func TestMeReportsTheFeatureAndKeepsItsShape(t *testing.T) {
	h := newTonightHarness(t, defaultLimits)

	me := h.decode(h.do(http.MethodGet, "/api/me", ""), http.StatusOK)
	if got := me["features"]; !reflect.DeepEqual(got, map[string]any{"recommendations": true}) {
		t.Errorf("features = %v, want recommendations on", got)
	}
	// The field is additive: the profile is still at the top level.
	if me["email"] != viewerEmail || me["name"] != "Sam Viewer" || me["region"] != "US" || me["id"] != h.user.ID.Hex() {
		t.Errorf("profile fields moved or changed: %v", me)
	}
	for _, key := range []string{"services", "preferences", "created_at", "updated_at"} {
		if _, ok := me[key]; !ok {
			t.Errorf("the profile lost its %q field: %v", key, me)
		}
	}
}

func TestTonightNeedsAToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := storetest.New()
	lister := watchlist.NewLister(store, nil, "US")
	model := &pickerModel{}
	s := &Server{db: store, lister: lister, defaultRegion: "US", recommender: tonight.New(lister, model, store, *defaultLimits)}
	router, err := s.routes(routeConfig{
		origins:  []string{"http://localhost:3000"},
		restAuth: func(c *gin.Context) { c.AbortWithStatus(http.StatusUnauthorized) },
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tonightPath, strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized || model.called() != 0 {
		t.Errorf("status = %d after %d model calls, want 401 and none", rec.Code, model.called())
	}
}

// The demo account is reserved: it cannot sign in, so it can never spend.
func TestTonightRefusesTheDemoAccount(t *testing.T) {
	h := newTonightHarness(t, defaultLimits)
	h.store.AddUser(&models.User{Email: DemoUserEmail, Region: "US", Services: h.user.Services})
	h.addTitles(6)

	for _, email := range []string{DemoUserEmail, "  Demo@WatchlistNotify.com "} {
		h.email = email
		h.ask(`{}`, http.StatusForbidden)
		h.decode(h.do(http.MethodGet, "/api/me", ""), http.StatusForbidden)
	}
	if h.model.called() != 0 || len(h.store.UsageKeys()) != 0 {
		t.Error("the demo account reached the model or the usage counters")
	}
}

// --- answers ---

func TestTonightResponseShape(t *testing.T) {
	h := newTonightHarness(t, defaultLimits)
	h.addTitles(6)
	h.model.answer = `{"picks":[{"index":4,"reason":"Quick and funny."},{"index":1,"reason":"A classic you keep putting off."},{"index":6,"reason":"Tense from the first scene."}]}`

	body := h.ask(`{"mood":"something light","max_minutes":120,"type":"movie"}`, http.StatusOK)

	// The titles are shuffled before they are numbered, so what index 4 means
	// is read from the prompt the model was given.
	numbered := promptTitles(t, h.model.prompt)
	if len(numbered) != 6 {
		t.Fatalf("the model was offered %d titles, want 6", len(numbered))
	}
	if got, want := pickedTitles(body), []string{numbered[4], numbered[1], numbered[6]}; !reflect.DeepEqual(got, want) {
		t.Fatalf("picks = %v, want %v", got, want)
	}
	if body["remaining_today"] != float64(9) || body["model"] != "claude-test-model" {
		t.Errorf("remaining_today = %v, model = %v; want 9 and the model's ID", body["remaining_today"], body["model"])
	}
	if _, has := body["message"]; has {
		t.Errorf("an answer with picks has a message: %v", body["message"])
	}

	first := body["picks"].([]any)[0].(map[string]any)
	if first["reason"] != "Quick and funny." {
		t.Errorf("reason = %v", first["reason"])
	}
	// The item is what GET /api/watchlist returns for it.
	item := first["item"].(map[string]any)
	list := h.decode(h.do(http.MethodGet, "/api/watchlist?limit=0", ""), http.StatusOK)
	var listed map[string]any
	for _, raw := range list["items"].([]any) {
		if raw.(map[string]any)["id"] == item["id"] {
			listed = raw.(map[string]any)
		}
	}
	if listed == nil || !reflect.DeepEqual(item, listed) {
		t.Errorf("item =\n%v\nwant the watchlist's\n%v", item, listed)
	}
	providers := item["availability"].(map[string]any)["providers"].([]any)
	if item["poster_path"] != "/poster.jpg" || len(providers) != 1 || providers[0].(map[string]any)["code"] != "netflix" {
		t.Errorf("item lacks its poster or the user's services: %v", item)
	}
}

func TestTonightAcceptsNoBody(t *testing.T) {
	for _, body := range []string{"", "{}", `{"mood":""}`, `{"exclude":[]}`, `{"unknown":1}`} {
		h := newTonightHarness(t, defaultLimits)
		h.addTitles(5)
		if got := h.ask(body, http.StatusOK); len(got["picks"].([]any)) != 3 {
			t.Errorf("body %q: picks = %v, want three", body, got["picks"])
		}
	}
}

func TestTonightPickAgainExcludesWhatWasShown(t *testing.T) {
	h := newTonightHarness(t, defaultLimits)
	h.addTitles(7)

	first := h.ask(`{}`, http.StatusOK)
	var shown []string
	for _, raw := range first["picks"].([]any) {
		shown = append(shown, raw.(map[string]any)["item"].(map[string]any)["id"].(string))
	}
	body, _ := json.Marshal(map[string]any{"exclude": shown})
	second := h.ask(string(body), http.StatusOK)

	again := map[string]bool{}
	for _, name := range pickedTitles(first) {
		again[name] = true
	}
	for _, name := range pickedTitles(second) {
		if again[name] {
			t.Errorf("%s was suggested twice", name)
		}
	}
	if len(pickedTitles(second)) != 3 || second["remaining_today"] != float64(8) {
		t.Errorf("second answer = %v with %v left, want three new picks and 8", pickedTitles(second), second["remaining_today"])
	}
	// None of the excluded titles was offered to the model.
	for _, name := range pickedTitles(first) {
		if strings.Contains(h.model.prompt, name) {
			t.Errorf("the excluded %s was sent to the model", name)
		}
	}
}

func TestTonightWithNothingToPick(t *testing.T) {
	t.Run("no services", func(t *testing.T) {
		h := newTonightHarness(t, defaultLimits)
		h.addTitles(6)
		h.user.Services = nil
		h.store.AddUser(h.user)

		body := h.ask(`{}`, http.StatusOK)
		if picks, ok := body["picks"].([]any); !ok || len(picks) != 0 {
			t.Errorf("picks = %v, want an empty array", body["picks"])
		}
		if body["empty_reason"] != tonight.EmptyNoServices || body["message"] == "" || body["message"] == nil {
			t.Errorf("body = %v, want the no-services reason and a message", body)
		}
		if body["remaining_today"] != float64(10) || body["model"] != "" {
			t.Errorf("remaining_today = %v, model = %q; want 10 and none", body["remaining_today"], body["model"])
		}
		if h.model.called() != 0 {
			t.Error("the model was called")
		}
	})

	t.Run("nothing streaming", func(t *testing.T) {
		h := newTonightHarness(t, defaultLimits)
		body := h.ask(`{}`, http.StatusOK)
		if body["empty_reason"] != tonight.EmptyNothingStreaming || len(body["picks"].([]any)) != 0 || h.model.called() != 0 {
			t.Errorf("body = %v after %d model calls", body, h.model.called())
		}
	})

	t.Run("three or fewer", func(t *testing.T) {
		h := newTonightHarness(t, defaultLimits)
		h.addTitles(3)
		body := h.ask(`{"mood":"anything"}`, http.StatusOK)
		if len(pickedTitles(body)) != 3 || body["model"] != "" || body["remaining_today"] != float64(10) {
			t.Errorf("body = %v, want the three titles, no model and nothing counted", body)
		}
		if h.model.called() != 0 || len(h.store.UsageKeys()) != 0 {
			t.Error("the model was called, or usage counted, with no choice to make")
		}
	})
}

// --- refusals and failures ---

func TestTonightInvalidRequests(t *testing.T) {
	manyIDs, _ := json.Marshal(make([]string, tonight.MaxExclude+1))
	cases := []struct {
		name, body, wantErr string
	}{
		{"not JSON", `mood=light`, "invalid body"},
		{"wrong field type", `{"mood":5}`, "invalid body"},
		{"array", `[]`, "invalid body"},
		{"mood too long", `{"mood":"` + strings.Repeat("a", tonight.MaxMoodLength+1) + `"}`, "mood is too long"},
		{"unknown type", `{"type":"tv"}`, "type must be"},
		{"zero minutes", `{"max_minutes":0}`, "max_minutes"},
		{"negative minutes", `{"max_minutes":-5}`, "max_minutes"},
		{"too many excluded", `{"exclude":` + string(manyIDs) + `}`, "exclude holds at most"},
		{"exclude that is not an id", `{"exclude":["Film 01"]}`, "exclude must hold"},
		{"body too large", `{"mood":"` + strings.Repeat("a", tonightMaxBody) + `"}`, "invalid body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTonightHarness(t, defaultLimits)
			h.addTitles(6)
			body := h.ask(tc.body, http.StatusBadRequest)
			if msg, _ := body["error"].(string); !strings.Contains(msg, tc.wantErr) {
				t.Errorf("error = %q, want it to mention %q", msg, tc.wantErr)
			}
			if h.model.called() != 0 || len(h.store.UsageKeys()) != 0 {
				t.Error("an invalid request reached the model or was counted")
			}
		})
	}
}

func TestTonightPerUserLimit(t *testing.T) {
	h := newTonightHarness(t, &tonight.Limits{PerUser: 2, Total: 200})
	h.addTitles(6)
	h.ask(`{}`, http.StatusOK)
	if body := h.ask(`{}`, http.StatusOK); body["remaining_today"] != float64(0) {
		t.Errorf("remaining_today = %v on the last allowed call, want 0", body["remaining_today"])
	}

	rec := h.do(http.MethodPost, tonightPath, `{}`)
	body := h.decode(rec, http.StatusTooManyRequests)

	resets, err := time.Parse(time.RFC3339, body["resets_at"].(string))
	if err != nil {
		t.Fatalf("resets_at = %v: %v", body["resets_at"], err)
	}
	now := time.Now().UTC()
	nextDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	// A test that straddles midnight sees the day after.
	if !resets.Equal(nextDay) && !resets.Equal(nextDay.AddDate(0, 0, 1)) {
		t.Errorf("resets_at = %s, want the next UTC midnight (%s)", resets, nextDay)
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "all 2 of today's picks") || !strings.Contains(msg, resets.Format(time.RFC3339)) {
		t.Errorf("error = %q, want it to give the limit and when it resets", msg)
	}
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retry < 1 || retry > 24*3600 {
		t.Errorf("Retry-After = %q, want the seconds until the reset", rec.Header().Get("Retry-After"))
	}
	if h.model.called() != 2 {
		t.Errorf("the model was called %d times with a limit of 2", h.model.called())
	}
}

func TestTonightTotalLimit(t *testing.T) {
	h := newTonightHarness(t, &tonight.Limits{PerUser: 10, Total: 1})
	h.addTitles(6)
	h.ask(`{}`, http.StatusOK)

	body := h.ask(`{}`, http.StatusTooManyRequests)
	if msg, _ := body["error"].(string); !strings.Contains(msg, "today's picks have all been used") {
		t.Errorf("error = %q, want the shared limit's message", msg)
	}
	if _, ok := body["resets_at"].(string); !ok {
		t.Errorf("body = %v, want resets_at", body)
	}
	if h.model.called() != 1 {
		t.Errorf("the model was called %d times with a total limit of 1", h.model.called())
	}
}

func TestTonightFailures(t *testing.T) {
	cases := []struct {
		name   string
		break_ func(h *tonightHarness)
		status int
		calls  int
	}{
		{"the model errors", func(h *tonightHarness) {
			h.model.err = &claude.APIError{StatusCode: 529, Type: "overloaded_error"}
		}, http.StatusBadGateway, 1},
		{"the model refuses", func(h *tonightHarness) { h.model.err = claude.ErrRefused }, http.StatusBadGateway, 1},
		{"the answer names no title", func(h *tonightHarness) { h.model.answer = `{"picks":[{"index":99,"reason":"x"}]}` }, http.StatusBadGateway, 1},
		{"the answer is not JSON", func(h *tonightHarness) { h.model.answer = `I pick Film 01.` }, http.StatusBadGateway, 1},
		{"the model times out", func(h *tonightHarness) {
			h.model.err = fmt.Errorf("claude: %w", context.DeadlineExceeded)
		}, http.StatusGatewayTimeout, 1},
		{"the watchlist cannot be read", func(h *tonightHarness) {
			h.store.Fail["ListWatchlistItems"] = errors.New("database is down")
		}, http.StatusInternalServerError, 0},
		{"the limit cannot be checked", func(h *tonightHarness) {
			h.store.Fail["ReserveUsage"] = errors.New("database is down")
		}, http.StatusInternalServerError, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTonightHarness(t, defaultLimits)
			h.addTitles(6)
			tc.break_(h)

			body := h.ask(`{}`, tc.status)
			if msg, _ := body["error"].(string); msg == "" || len(body) != 1 {
				t.Errorf("body = %v, want only an error message", body)
			}
			if _, has := body["picks"]; has {
				t.Errorf("a failure returned picks: %v", body)
			}
			if h.model.called() != tc.calls {
				t.Errorf("the model was called %d times, want %d", h.model.called(), tc.calls)
			}
		})
	}
}

// Neither what the viewer typed nor what the model said may reach the logs,
// whether the request succeeds or fails.
func TestTonightLogsNeitherMoodNorAnswer(t *testing.T) {
	const (
		mood   = "MOOD-zebra-crossing"
		reason = "REASON-quiet-harbour"
	)
	request := `{"mood":"` + mood + `"}`
	answers := []struct {
		name   string
		answer string
		err    error
		status int
	}{
		{"success", `{"picks":[{"index":1,"reason":"` + reason + `"}]}`, nil, http.StatusOK},
		{"no usable pick", `{"picks":[{"index":42,"reason":"` + reason + `"}]}`, nil, http.StatusBadGateway},
		{"not JSON", reason + ` is what I would say`, nil, http.StatusBadGateway},
		{"model error", "", &claude.APIError{StatusCode: 500, Type: "api_error", RequestID: "req_1"}, http.StatusBadGateway},
	}
	for _, tc := range answers {
		t.Run(tc.name, func(t *testing.T) {
			logged := captureLog(t)
			h := newTonightHarness(t, defaultLimits)
			h.addTitles(6)
			h.model.answer, h.model.err = tc.answer, tc.err

			rec := h.do(http.MethodPost, tonightPath, request)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, tc.status, rec.Body)
			}
			if !strings.Contains(h.model.prompt, mood) {
				t.Fatal("the mood never reached the model; the test proves nothing")
			}
			for _, secret := range []string{mood, reason} {
				if strings.Contains(logged.String(), secret) {
					t.Errorf("the log contains %q:\n%s", secret, logged)
				}
			}
			// A failure is logged, with what is safe to say about it.
			if tc.status != http.StatusOK && !strings.Contains(logged.String(), "tonight: ") {
				t.Errorf("the failure was not logged:\n%s", logged)
			}
			// And a failure tells the client nothing the model said.
			if tc.status != http.StatusOK && strings.Contains(rec.Body.String(), reason) {
				t.Errorf("the error response repeats the model's answer: %s", rec.Body)
			}
		})
	}
}

// --- configuration ---

func TestLoadTonightSettings(t *testing.T) {
	env := func(pairs ...string) func(string) string {
		values := map[string]string{}
		for i := 0; i < len(pairs); i += 2 {
			values[pairs[i]] = pairs[i+1]
		}
		return func(name string) string { return values[name] }
	}
	key := "ANTHROPIC_API_KEY"

	cases := []struct {
		name    string
		getenv  func(string) string
		want    tonightSettings
		wantOff string // part of the reason; empty when the feature is on
	}{
		{name: "nothing set", getenv: env(), wantOff: "ANTHROPIC_API_KEY is not set"},
		{name: "blank key", getenv: env(key, "  "), wantOff: "ANTHROPIC_API_KEY is not set"},
		{name: "limits without a key", getenv: env("AI_DAILY_LIMIT_TOTAL", "500"), wantOff: "ANTHROPIC_API_KEY is not set"},
		{name: "key only", getenv: env(key, " sk-ant-x "),
			want: tonightSettings{apiKey: "sk-ant-x", limits: tonight.Limits{PerUser: 10, Total: 200}}},
		{name: "everything", getenv: env(key, "sk-ant-x", "ANTHROPIC_MODEL", " claude-sonnet-5-5 ", "AI_DAILY_LIMIT_PER_USER", "3", "AI_DAILY_LIMIT_TOTAL", " 50 "),
			want: tonightSettings{apiKey: "sk-ant-x", model: "claude-sonnet-5-5", limits: tonight.Limits{PerUser: 3, Total: 50}}},
		// A limit of zero switches the feature off rather than leaving a
		// panel that always says the limit is reached.
		{name: "total of zero", getenv: env(key, "sk-ant-x", "AI_DAILY_LIMIT_TOTAL", "0"), wantOff: "AI_DAILY_LIMIT_TOTAL is 0"},
		{name: "per user of zero", getenv: env(key, "sk-ant-x", "AI_DAILY_LIMIT_PER_USER", "0"), wantOff: "AI_DAILY_LIMIT_PER_USER is 0"},
		// A limit that cannot be read never becomes "no limit" or the default.
		{name: "not a number", getenv: env(key, "sk-ant-x", "AI_DAILY_LIMIT_TOTAL", "lots"), wantOff: "AI_DAILY_LIMIT_TOTAL is not a whole number"},
		{name: "negative", getenv: env(key, "sk-ant-x", "AI_DAILY_LIMIT_PER_USER", "-1"), wantOff: "AI_DAILY_LIMIT_PER_USER is not a whole number"},
		{name: "fraction", getenv: env(key, "sk-ant-x", "AI_DAILY_LIMIT_PER_USER", "2.5"), wantOff: "AI_DAILY_LIMIT_PER_USER is not a whole number"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok, reason := loadTonightSettings(tc.getenv)
			if tc.wantOff != "" {
				if ok || !strings.Contains(reason, tc.wantOff) || got != (tonightSettings{}) {
					t.Errorf("settings = %+v, on = %v, reason = %q; want off because %q", got, ok, reason, tc.wantOff)
				}
				return
			}
			if !ok || got != tc.want {
				t.Errorf("settings = %+v, on = %v (%s); want %+v", got, ok, reason, tc.want)
			}
		})
	}
}

func TestNewRecommenderFromTheEnvironment(t *testing.T) {
	const key = "sk-ant-secret-0123456789"
	store := storetest.New()
	lister := watchlist.NewLister(store, nil, "US")
	logged := captureLog(t)

	if rec := newRecommender(lister, store, func(string) string { return "" }); rec != nil {
		t.Error("a recommender was built without a key")
	}
	if !strings.Contains(logged.String(), "recommendations: off (ANTHROPIC_API_KEY is not set)") {
		t.Errorf("the log does not say the feature is off:\n%s", logged)
	}

	rec := newRecommender(lister, store, func(name string) string {
		return map[string]string{"ANTHROPIC_API_KEY": key, "AI_DAILY_LIMIT_TOTAL": "25"}[name]
	})
	if rec == nil {
		t.Fatal("no recommender was built with a key")
	}
	// The cheapest current model unless ANTHROPIC_MODEL says otherwise.
	if rec.Model() != claude.DefaultModel || rec.Model() != "claude-haiku-5-5" {
		t.Errorf("model = %q, want %q", rec.Model(), "claude-haiku-5-5")
	}
	if rec.Limits() != (tonight.Limits{PerUser: 10, Total: 25}) {
		t.Errorf("limits = %+v, want 10 per user and 25 in total", rec.Limits())
	}
	if strings.Contains(logged.String(), key) || strings.Contains(logged.String(), "sk-ant") {
		t.Errorf("the key was logged:\n%s", logged)
	}
	if !strings.Contains(logged.String(), "recommendations: on, model claude-haiku-5-5, at most 10 calls a day per user and 25 in total") {
		t.Errorf("the log does not describe the configuration:\n%s", logged)
	}
}
