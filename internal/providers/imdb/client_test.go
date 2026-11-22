package imdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseSource(t *testing.T) {
	valid := map[string]string{
		"ur101082417":     "ur101082417",
		"ls042518938":     "ls042518938",
		"  ls042518938\n": "ls042518938",
		"https://www.imdb.com/user/ur101082417/watchlist":                       "ur101082417",
		"https://www.imdb.com/user/ur101082417/watchlist/?ref_=nv_usr_wl_all_0": "ur101082417",
		"https://www.imdb.com/list/ls042518938/":                                "ls042518938",
		"https://www.imdb.com/list/ls042518938":                                 "ls042518938",
		"http://imdb.com/list/ls042518938/?sort=list_order,asc#top":             "ls042518938",
		"https://m.imdb.com/list/ls042518938/":                                  "ls042518938",
		"www.imdb.com/list/ls042518938/":                                        "ls042518938",
		"imdb.com/user/ur101082417/watchlist":                                   "ur101082417",
		"https://www.imdb.com/de/list/ls042518938/":                             "ls042518938",
	}
	for input, want := range valid {
		got, err := ParseSource(input)
		if err != nil || got != want {
			t.Errorf("ParseSource(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	invalid := []string{
		"",
		"   ",
		"tt0133093",                              // a title, not a list
		"nm0000206",                              // a person
		"ur1234",                                 // too short
		"ls1234567890123",                        // too long
		"ls04251893x",                            // not digits
		"UR101082417",                            // IDs are lower case
		"ls042518938; drop",                      // trailing junk
		"ur101082417/watchlist",                  // neither a bare ID nor an IMDb URL
		"https://www.imdb.com/title/tt0133093/",  // an IMDb page without a list
		"https://www.imdb.com/",                  // no path
		"https://www.imdb.com/?list=ls042518938", // only the path counts
		"https://example.com/list/ls042518938/",  // not IMDb
		"https://imdb.com.example.com/list/ls042518938/",
		"https://notimdb.com/list/ls042518938/",
		"ftp://www.imdb.com/list/ls042518938/",
		"javascript:alert(1)//imdb.com/list/ls042518938",
		"https://www.imdb.com/list/" + strings.Repeat("a", 600),
	}
	for _, input := range invalid {
		if got, err := ParseSource(input); !errors.Is(err, ErrInvalidSource) || got != "" {
			t.Errorf("ParseSource(%q) = %q, %v; want ErrInvalidSource", input, got, err)
		}
	}
}

func TestValidTitleID(t *testing.T) {
	for id, want := range map[string]bool{
		"tt0133093": true, "tt12345678": true,
		"": false, "tt": false, "tt123": false, "nm0000206": false, "TT0133093": false, "tt0133093 ": false, "tt01330x3": false,
	} {
		if got := ValidTitleID(id); got != want {
			t.Errorf("ValidTitleID(%q) = %v, want %v", id, got, want)
		}
	}
}

// --- a fake IMDb ---

// recorded is one request the fake received.
type recorded struct {
	Header    http.Header
	Query     string
	Variables map[string]any
}

// fakeIMDb serves a queue of responses, one per request, and records the
// requests. Running out of responses fails the test.
type fakeIMDb struct {
	t         *testing.T
	mu        sync.Mutex
	responses []response
	requests  []recorded
	server    *httptest.Server
}

type response struct {
	status int
	header map[string]string
	body   string
}

func newFakeIMDb(t *testing.T, responses ...response) *fakeIMDb {
	f := &fakeIMDb{t: t, responses: responses}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeIMDb) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodPost {
		f.t.Errorf("method = %s, want POST", r.Method)
	}
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		f.t.Errorf("request body is not JSON: %v: %s", err, raw)
	}
	f.requests = append(f.requests, recorded{Header: r.Header.Clone(), Query: req.Query, Variables: req.Variables})

	if len(f.responses) == 0 {
		f.t.Errorf("unexpected request %d", len(f.requests))
		w.WriteHeader(http.StatusTeapot)
		return
	}
	next := f.responses[0]
	f.responses = f.responses[1:]
	for key, value := range next.header {
		w.Header().Set(key, value)
	}
	if next.status == 0 {
		next.status = http.StatusOK
	}
	w.WriteHeader(next.status)
	_, _ = io.WriteString(w, next.body)
}

func (f *fakeIMDb) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// client returns a Client pointed at the fake that records its backoff
// instead of sleeping.
func (f *fakeIMDb) client() (*Client, *[]time.Duration) {
	var waits []time.Duration
	c := New(WithBaseURL(f.server.URL), WithHTTPClient(f.server.Client()))
	c.sleep = func(ctx context.Context, d time.Duration) error {
		waits = append(waits, d)
		return ctx.Err()
	}
	return c, &waits
}

// title is a list entry that is a title; other is one that is not.
func title(id string) string {
	return fmt.Sprintf(`{"__typename":"Title","id":%q,"titleText":{"text":"Title %s"},"titleType":{"id":"movie","isSeries":false}}`, id, id)
}

const (
	person     = `{"__typename":"Name"}`
	emptyNode  = `{}`
	nullNode   = `null`
	listField  = "listItem"
	watchField = "item"
)

// page builds one response page. root and field select the query shape;
// cursor is the endCursor ("" on a last page means no further page).
func page(root, field string, total int, cursor string, nodes ...string) response {
	edges := make([]string, len(nodes))
	for i, node := range nodes {
		edges[i] = fmt.Sprintf(`{"node":{%q:%s}}`, field, node)
	}
	return response{body: fmt.Sprintf(
		`{"data":{%q:{"items":{"total":%d,"pageInfo":{"hasNextPage":%t,"endCursor":%q},"edges":[%s]}}},"extensions":{"disclaimer":"…"}}`,
		root, total, cursor != "", cursor, strings.Join(edges, ","),
	)}
}

func listPage(total int, cursor string, nodes ...string) response {
	return page("list", listField, total, cursor, nodes...)
}

func ids(list List) []string {
	out := []string{}
	for _, e := range list.Entries {
		out = append(out, e.IMDbID)
	}
	return out
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// --- reading a list ---

// The fixture is a response recorded from IMDb, cut down to three entries.
func TestFetchListReadsARecordedResponse(t *testing.T) {
	fake := newFakeIMDb(t, response{body: fixture(t, "list_page.json")})
	client, _ := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	want := List{
		SourceID: "ls042518938",
		Entries: []Entry{
			{IMDbID: "tt0031381", Title: "Gone with the Wind", TypeID: "movie"},
			{IMDbID: "tt0056172", Title: "Lawrence of Arabia", TypeID: "movie"},
			{IMDbID: "tt0903747", Title: "Breaking Bad", TypeID: "tvSeries", IsSeries: true},
		},
		Total:    3,
		Complete: true,
	}
	if !reflect.DeepEqual(list, want) {
		t.Errorf("list =\n%+v\nwant\n%+v", list, want)
	}

	req := fake.requests[0]
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := req.Header.Get("X-Imdb-Client-Name"); got != "imdb-web-next" {
		t.Errorf("x-imdb-client-name = %q", got)
	}
	if got := req.Header.Get("User-Agent"); !strings.HasPrefix(got, "Mozilla/5.0") {
		t.Errorf("User-Agent = %q, want a browser's", got)
	}
	if !strings.Contains(req.Query, "list(id: $id)") || !strings.Contains(req.Query, "listItem") || !strings.Contains(req.Query, "first: 250") {
		t.Errorf("query for a list ID = %s", req.Query)
	}
	if !reflect.DeepEqual(req.Variables, map[string]any{"id": "ls042518938", "after": nil}) {
		t.Errorf("variables = %v", req.Variables)
	}
}

func TestFetchListByUserIDAsksForTheWatchlist(t *testing.T) {
	fake := newFakeIMDb(t, page("predefinedList", watchField, 2, "", title("tt0000001"), title("tt0000002")))
	client, _ := fake.client()

	list, err := client.FetchList(context.Background(), "ur101082417")
	if err != nil || !list.Complete || !reflect.DeepEqual(ids(list), []string{"tt0000001", "tt0000002"}) {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	req := fake.requests[0]
	if !strings.Contains(req.Query, "predefinedList(classType: WATCH_LIST, userId: $user)") || strings.Contains(req.Query, "listItem") {
		t.Errorf("query for a user ID = %s", req.Query)
	}
	if !reflect.DeepEqual(req.Variables, map[string]any{"user": "ur101082417", "after": nil}) {
		t.Errorf("variables = %v", req.Variables)
	}
}

func TestFetchListFollowsPages(t *testing.T) {
	fake := newFakeIMDb(t,
		listPage(5, "cursor-1", title("tt0000001"), title("tt0000002")),
		listPage(5, "cursor-2", title("tt0000003"), title("tt0000004")),
		listPage(5, "", title("tt0000005")),
	)
	client, waits := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	if err != nil {
		t.Fatalf("FetchList: %v", err)
	}
	if got, want := ids(list), []string{"tt0000001", "tt0000002", "tt0000003", "tt0000004", "tt0000005"}; !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v, want %v in list order", got, want)
	}
	if !list.Complete || list.Total != 5 {
		t.Errorf("complete = %v, total = %d; want a complete list of 5", list.Complete, list.Total)
	}
	var cursors []any
	for _, req := range fake.requests {
		cursors = append(cursors, req.Variables["after"])
	}
	if want := []any{nil, "cursor-1", "cursor-2"}; !reflect.DeepEqual(cursors, want) {
		t.Errorf("cursors sent = %v, want %v", cursors, want)
	}
	if len(*waits) != 0 {
		t.Errorf("a clean fetch backed off: %v", *waits)
	}
}

// Complete is what the sync's remove rule rests on. Each case is one page
// (or two) and whether the list may be called complete.
func TestFetchListCompleteness(t *testing.T) {
	tests := []struct {
		name         string
		pages        []response
		wantEntries  int
		wantOther    int
		wantRepeated int
		wantComplete bool
	}{
		{
			name:         "every entry is a title",
			pages:        []response{listPage(2, "", title("tt0000001"), title("tt0000002"))},
			wantEntries:  2,
			wantComplete: true,
		},
		{
			name:         "an empty list",
			pages:        []response{listPage(0, "")},
			wantComplete: true,
		},
		{
			// A list of people and titles: the people count towards the total
			// and must not make a whole fetch look partial.
			name:         "entries that are not titles are counted",
			pages:        []response{listPage(3, "", title("tt0000001"), person, title("tt0000002"))},
			wantEntries:  2,
			wantOther:    1,
			wantComplete: true,
		},
		{
			name:         "fewer entries than the total",
			pages:        []response{listPage(5, "", title("tt0000001"), title("tt0000002"))},
			wantEntries:  2,
			wantComplete: false,
		},
		{
			name:         "more entries than the total",
			pages:        []response{listPage(1, "", title("tt0000001"), title("tt0000002"))},
			wantEntries:  2,
			wantComplete: false,
		},
		{
			// An entry with no type and no ID is not "a person": it is an
			// entry IMDb failed to describe, and it could be any title.
			name:         "an entry that could not be read",
			pages:        []response{listPage(2, "", title("tt0000001"), emptyNode)},
			wantEntries:  1,
			wantComplete: false,
		},
		{
			name:         "a null entry",
			pages:        []response{listPage(2, "", title("tt0000001"), nullNode)},
			wantEntries:  1,
			wantComplete: false,
		},
		{
			name:         "a title with a malformed ID",
			pages:        []response{listPage(2, "", title("tt0000001"), title("nm0000206"))},
			wantEntries:  1,
			wantComplete: false,
		},
		{
			name:         "a title without an ID",
			pages:        []response{listPage(2, "", title("tt0000001"), `{"__typename":"Title"}`)},
			wantEntries:  1,
			wantComplete: false,
		},
		{
			// The list was edited between two pages: the count adds up to the
			// latest total, but the pages are not one view of the list.
			name: "the total changed between pages",
			pages: []response{
				listPage(4, "cursor-1", title("tt0000001"), title("tt0000002")),
				listPage(3, "", title("tt0000003")),
			},
			wantEntries:  3,
			wantComplete: false,
		},
		{
			// Two pages that overlap: four entries arrive for a list of four,
			// and tt0000003 was never read.
			name: "overlapping pages that add up to the total",
			pages: []response{
				listPage(4, "cursor-1", title("tt0000001"), title("tt0000002")),
				listPage(4, "", title("tt0000002"), title("tt0000004")),
			},
			wantEntries:  3,
			wantRepeated: 1,
			wantComplete: false,
		},
		{
			// Every title arrived, and one of them twice: the pages are not a
			// clean walk over the list, so nothing is concluded from them.
			name: "a repeat although the distinct titles add up",
			pages: []response{
				listPage(3, "cursor-1", title("tt0000001"), title("tt0000002")),
				listPage(3, "", title("tt0000002"), title("tt0000003")),
			},
			wantEntries:  3,
			wantRepeated: 1,
			wantComplete: false,
		},
		{
			// Indistinguishable from an overlap, so it is treated the same.
			name:         "a list that holds a title twice",
			pages:        []response{listPage(3, "", title("tt0000001"), title("tt0000002"), title("tt0000001"))},
			wantEntries:  2,
			wantRepeated: 1,
			wantComplete: false,
		},
		{
			// The repeat hides a title even when a person fills the count.
			name: "a repeat next to entries that are not titles",
			pages: []response{
				listPage(4, "cursor-1", title("tt0000001"), person),
				listPage(4, "", title("tt0000001"), title("tt0000002")),
			},
			wantEntries:  2,
			wantOther:    1,
			wantRepeated: 1,
			wantComplete: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeIMDb(t, tt.pages...)
			client, _ := fake.client()
			list, err := client.FetchList(context.Background(), "ls042518938")
			if err != nil {
				t.Fatalf("FetchList: %v", err)
			}
			if len(list.Entries) != tt.wantEntries || list.Other != tt.wantOther || list.Repeated != tt.wantRepeated || list.Complete != tt.wantComplete {
				t.Errorf("entries = %d, other = %d, repeated = %d, complete = %v; want %d, %d, %d, %v",
					len(list.Entries), list.Other, list.Repeated, list.Complete, tt.wantEntries, tt.wantOther, tt.wantRepeated, tt.wantComplete)
			}
		})
	}
}

// --- lists that are not there ---

func TestFetchListNotFound(t *testing.T) {
	tests := map[string]string{
		"recorded response for a missing list":  "", // filled from the fixture below
		"null list without errors":              `{"data":{"list":null}}`,
		"no data at all":                        `{"data":null}`,
		"a client error such as a private list": `{"errors":[{"message":"x","extensions":{"code":"FORBIDDEN","errorType":"CLIENT","isRetryable":false}}],"data":{"list":null}}`,
		"list without items":                    `{"data":{"list":{"id":"ls042518938","items":null}}}`,
	}
	tests["recorded response for a missing list"] = fixture(t, "list_not_found.json")

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			fake := newFakeIMDb(t, response{body: body})
			client, waits := fake.client()
			list, err := client.FetchList(context.Background(), "ls042518938")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if list.Complete || len(list.Entries) != 0 {
				t.Errorf("a missing list returned %+v", list)
			}
			if fake.requestCount() != 1 || len(*waits) != 0 {
				t.Errorf("a missing list was asked for %d times", fake.requestCount())
			}
		})
	}
}

// IMDb failing is not the user's list being private.
func TestFetchListServerSideGraphQLErrorIsNotNotFound(t *testing.T) {
	body := `{"errors":[{"message":"upstream exploded at host 10.0.0.1","extensions":{"code":"INTERNAL_SERVER_ERROR","errorType":"SERVER","isRetryable":false}}],"data":{"list":null}}`
	fake := newFakeIMDb(t, response{body: body})
	client, _ := fake.client()

	_, err := client.FetchList(context.Background(), "ls042518938")
	var apiErr *APIError
	if errors.Is(err, ErrNotFound) || !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want an APIError that is not ErrNotFound", err)
	}
	if got := err.Error(); got != "imdb: graphql error INTERNAL_SERVER_ERROR" {
		t.Errorf("error text = %q; want the code only, never IMDb's message", got)
	}
	if fake.requestCount() != 1 {
		t.Errorf("an error IMDb marks as not retryable was retried: %d requests", fake.requestCount())
	}
}

func TestFetchListDoesNotTrustAPageThatCameWithErrors(t *testing.T) {
	body := `{"errors":[{"message":"x","extensions":{"code":"DOWNSTREAM_ERROR","errorType":"SERVER","isRetryable":true}}],` +
		`"data":{"list":{"items":{"total":1,"pageInfo":{"hasNextPage":false,"endCursor":""},"edges":[{"node":{"listItem":null}}]}}}}`
	fake := newFakeIMDb(t, response{body: body}, listPage(1, "", title("tt0000001")))
	client, waits := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	if err != nil || !list.Complete || len(list.Entries) != 1 {
		t.Fatalf("list = %+v, err = %v; want the retried page", list, err)
	}
	if len(*waits) != 1 {
		t.Errorf("backoffs = %v, want one before the retry", *waits)
	}
}

func TestErrorCodesAreSanitized(t *testing.T) {
	body := `{"errors":[{"extensions":{"code":"see https://evil.example/?token=abc","errorType":"SERVER"}}],"data":{"list":null}}`
	fake := newFakeIMDb(t, response{body: body})
	client, _ := fake.client()
	_, err := client.FetchList(context.Background(), "ls042518938")
	if err == nil || err.Error() != "imdb: graphql error UNKNOWN" {
		t.Errorf("err = %v; want a code that is not IMDb's free text", err)
	}
}

// --- failures and retries ---

func TestFetchListRetriesTransientFailures(t *testing.T) {
	fake := newFakeIMDb(t,
		response{status: http.StatusServiceUnavailable},
		response{status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "7"}},
		listPage(1, "", title("tt0000001")),
	)
	client, waits := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	if err != nil || !list.Complete {
		t.Fatalf("list = %+v, err = %v", list, err)
	}
	if want := []time.Duration{time.Second, 7 * time.Second}; !reflect.DeepEqual(*waits, want) {
		t.Errorf("backoffs = %v, want %v (growing, and honouring Retry-After)", *waits, want)
	}
}

func TestFetchListGivesUpAfterBoundedAttempts(t *testing.T) {
	fake := newFakeIMDb(t,
		response{status: http.StatusBadGateway},
		response{status: http.StatusBadGateway},
		response{status: http.StatusBadGateway, header: map[string]string{"Retry-After": "3600"}},
	)
	client, waits := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("err = %v, want the 502", err)
	}
	if err.Error() != "imdb: status 502" {
		t.Errorf("error text = %q", err.Error())
	}
	if list.Complete || len(list.Entries) != 0 {
		t.Errorf("a failed fetch returned %+v", list)
	}
	if fake.requestCount() != defaultMaxAttempts || len(*waits) != defaultMaxAttempts-1 {
		t.Errorf("%d requests and %d backoffs, want %d and %d", fake.requestCount(), len(*waits), defaultMaxAttempts, defaultMaxAttempts-1)
	}
}

func TestFetchListCapsTheRetryWait(t *testing.T) {
	fake := newFakeIMDb(t,
		response{status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "3600"}},
		listPage(0, ""),
	)
	client, waits := fake.client()
	if _, err := client.FetchList(context.Background(), "ls042518938"); err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{maxRetryWait}; !reflect.DeepEqual(*waits, want) {
		t.Errorf("backoffs = %v, want %v", *waits, want)
	}
}

func TestFetchListDoesNotRetryWhatCannotImprove(t *testing.T) {
	tests := map[string]response{
		"a 4xx":          {status: http.StatusForbidden},
		"malformed JSON": {body: `<html>captcha</html>`},
	}
	for name, resp := range tests {
		t.Run(name, func(t *testing.T) {
			fake := newFakeIMDb(t, resp)
			client, waits := fake.client()
			_, err := client.FetchList(context.Background(), "ls042518938")
			if err == nil || errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want a failure that is not ErrNotFound", err)
			}
			if fake.requestCount() != 1 || len(*waits) != 0 {
				t.Errorf("%d requests, %d backoffs; want a single attempt", fake.requestCount(), len(*waits))
			}
		})
	}
}

// A failure after the first page leaves what was read, marked incomplete.
func TestFetchListKeepsWhatWasReadWhenALaterPageFails(t *testing.T) {
	fake := newFakeIMDb(t,
		listPage(4, "cursor-1", title("tt0000001"), title("tt0000002")),
		response{status: http.StatusInternalServerError},
		response{status: http.StatusInternalServerError},
		response{status: http.StatusInternalServerError},
	)
	client, _ := fake.client()

	list, err := client.FetchList(context.Background(), "ls042518938")
	if err == nil {
		t.Fatal("a fetch that lost its second page reported no error")
	}
	if list.Complete {
		t.Error("a fetch that lost its second page is complete")
	}
	if got, want := ids(list), []string{"tt0000001", "tt0000002"}; !reflect.DeepEqual(got, want) || list.Total != 4 {
		t.Errorf("entries = %v, total = %d; want the first page kept and the total of 4", got, list.Total)
	}
}

func TestFetchListStopsWhenPagingDoesNotAdvance(t *testing.T) {
	tests := map[string][]response{
		"the same cursor twice": {
			listPage(4, "cursor-1", title("tt0000001")),
			listPage(4, "cursor-1", title("tt0000002")),
		},
		"a cursor from two pages back": {
			listPage(6, "cursor-1", title("tt0000001")),
			listPage(6, "cursor-2", title("tt0000002")),
			listPage(6, "cursor-1", title("tt0000003")),
			listPage(6, "", title("tt0000004"), title("tt0000005"), title("tt0000006")),
		},
		"more pages promised without a cursor": {
			{body: `{"data":{"list":{"items":{"total":4,"pageInfo":{"hasNextPage":true,"endCursor":""},"edges":[]}}}}`},
		},
	}
	for name, pages := range tests {
		t.Run(name, func(t *testing.T) {
			fake := newFakeIMDb(t, pages...)
			client, _ := fake.client()
			list, err := client.FetchList(context.Background(), "ls042518938")
			if err == nil || list.Complete {
				t.Errorf("list = %+v, err = %v; want an error and an incomplete list", list, err)
			}
		})
	}
}

func TestFetchListRefusesAnEndlessList(t *testing.T) {
	fake := newFakeIMDb(t,
		listPage(9, "cursor-1", title("tt0000001")),
		listPage(9, "cursor-2", title("tt0000002")),
	)
	client, _ := fake.client()
	client.maxPages = 2

	list, err := client.FetchList(context.Background(), "ls042518938")
	if !errors.Is(err, ErrTooLarge) || list.Complete {
		t.Errorf("list = %+v, err = %v; want ErrTooLarge", list, err)
	}
	if fake.requestCount() != 2 {
		t.Errorf("%d requests, want it to stop at the page limit", fake.requestCount())
	}
}

func TestFetchListRejectsAnIDItWasNotGiven(t *testing.T) {
	fake := newFakeIMDb(t)
	client, _ := fake.client()
	for _, id := range []string{"", "tt0133093", "ls042518938\"}", "https://www.imdb.com/list/ls042518938/"} {
		if _, err := client.FetchList(context.Background(), id); !errors.Is(err, ErrInvalidSource) {
			t.Errorf("FetchList(%q) err = %v, want ErrInvalidSource", id, err)
		}
	}
	if fake.requestCount() != 0 {
		t.Errorf("an invalid ID reached IMDb %d times", fake.requestCount())
	}
}

func TestFetchListHonoursTheContext(t *testing.T) {
	t.Run("already cancelled", func(t *testing.T) {
		fake := newFakeIMDb(t)
		client, waits := fake.client()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.FetchList(ctx, "ls042518938"); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
		if len(*waits) != 0 {
			t.Errorf("a cancelled fetch backed off %v", *waits)
		}
	})

	t.Run("cancelled while backing off", func(t *testing.T) {
		fake := newFakeIMDb(t, response{status: http.StatusServiceUnavailable})
		client, _ := fake.client()
		ctx, cancel := context.WithCancel(context.Background())
		client.sleep = func(ctx context.Context, _ time.Duration) error {
			cancel()
			return ctx.Err()
		}
		_, err := client.FetchList(ctx, "ls042518938")
		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("err = %v, want the failure that was being retried", err)
		}
		if fake.requestCount() != 1 {
			t.Errorf("%d requests after cancellation, want 1", fake.requestCount())
		}
	})

	t.Run("a network error does not leak the URL", func(t *testing.T) {
		fake := newFakeIMDb(t)
		client, _ := fake.client()
		fake.server.Close()
		_, err := client.FetchList(context.Background(), "ls042518938")
		if err == nil || strings.Contains(err.Error(), fake.server.URL) {
			t.Errorf("err = %v; want a network error without the endpoint URL", err)
		}
		if fake.requestCount() != 0 {
			t.Errorf("requests = %d", fake.requestCount())
		}
	})
}
