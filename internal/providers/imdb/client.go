// Package imdb reads a public IMDb list: a user's Watchlist or a list they
// made. It is the only code in this service that talks to IMDb.
//
// # Terms of use
//
// IMDb has no public API for lists. This client calls the GraphQL endpoint
// IMDb's own website uses, which answers without a login, and every response
// from it carries this notice: "Public, commercial, and/or non-private use of
// the IMDb data provided by this API is not allowed." The rest of the service
// is built around taking that seriously:
//
//   - It is opt-in. A list is fetched only for a user who linked it, and that
//     user chose to make the list public.
//   - Only IMDb title IDs are kept. Titles, years, posters and everything
//     else shown to users come from TMDb. Entry.Title and Entry.TypeID exist
//     for logs and are never stored.
//   - One fetch per linked user per day, from the worker; linking a list is
//     that day's fetch. There is no "sync now" and no fan-out.
//   - One request at a time. Pages of a list are read one after another, and
//     the worker syncs users one after another.
//
// Because the endpoint is undocumented it can change or disappear without
// notice. The client therefore distrusts what it reads: FetchList reports a
// list as Complete only when every page arrived and the count adds up, and
// the sync removes nothing on the strength of a fetch that is not Complete.
package imdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.graphql.imdb.com/"
	// The endpoint answers only to what looks like IMDb's own web client.
	clientName = "imdb-web-next"
	userAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128 Safari/537.36"

	// pageSize is the most entries the endpoint returns per request.
	pageSize = 250
	// defaultMaxPages bounds one fetch: 40 pages are 10,000 entries.
	defaultMaxPages = 40
	// MaxListSize is the largest list FetchList reads.
	MaxListSize = defaultMaxPages * pageSize

	defaultMaxAttempts = 3
	maxRetryWait       = 15 * time.Second
	maxResponseBytes   = 4 << 20
)

var (
	// ErrInvalidSource is returned by ParseSource for input that is not an
	// IMDb user ID, list ID or URL containing one.
	ErrInvalidSource = errors.New("imdb: not a user ID (ur…), a list ID (ls…) or an IMDb URL containing one")
	// ErrNotFound means IMDb has no such list, or has it and will not show it
	// because it is private. IMDb does not tell the two apart.
	ErrNotFound = errors.New("imdb: list not found or not public")
	// ErrTooLarge means the list has more than MaxListSize entries.
	ErrTooLarge = errors.New("imdb: list is too large")
)

var (
	sourceIDPattern = regexp.MustCompile(`^(ur|ls)\d{5,12}$`)
	titleIDPattern  = regexp.MustCompile(`^tt\d{5,12}$`)
)

// ValidTitleID reports whether id looks like an IMDb title ID (tt0133093).
func ValidTitleID(id string) bool {
	return titleIDPattern.MatchString(id)
}

// ParseSource extracts the ID of a list from what a user typed or pasted: a
// bare user ID (ur101082417, meaning that user's Watchlist), a bare list ID
// (ls042518938), or an imdb.com URL with one of them in its path, such as
// https://www.imdb.com/user/ur101082417/watchlist or
// https://www.imdb.com/list/ls042518938/.
//
// Anything else is ErrInvalidSource. The result always matches
// ^(ur|ls)\d{5,12}$, so it is safe to store, log and send on.
func ParseSource(input string) (string, error) {
	input = strings.TrimSpace(input)
	if sourceIDPattern.MatchString(input) {
		return input, nil
	}
	if input == "" || len(input) > 512 {
		return "", ErrInvalidSource
	}
	raw := input
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw // "www.imdb.com/list/…" pasted without a scheme
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ErrInvalidSource
	}
	host := strings.ToLower(u.Hostname())
	if host != "imdb.com" && !strings.HasSuffix(host, ".imdb.com") {
		return "", ErrInvalidSource
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if sourceIDPattern.MatchString(segment) {
			return segment, nil
		}
	}
	return "", ErrInvalidSource
}

// Entry is one title on a list.
type Entry struct {
	IMDbID string // tt0133093
	// Title and TypeID are IMDb's display title and title type (movie,
	// tvSeries, tvEpisode, videoGame, …). They are for logs; the service
	// stores neither.
	Title    string
	TypeID   string
	IsSeries bool
}

// List is the result of FetchList.
type List struct {
	SourceID string
	// Entries are the titles that were read, in list order, each once.
	Entries []Entry
	// Total is the number of entries IMDb says the list has.
	Total int
	// Other counts entries that were read and are not titles (a list can
	// hold people, for one). They count towards Total.
	Other int
	// Repeated counts title entries whose ID had already been read.
	Repeated int
	// Complete reports that the whole list was read: every page arrived, the
	// last one said there were no more, IMDb gave the same Total throughout,
	// every entry could be understood, no title came twice, and the distinct
	// titles plus the other entries add up to Total. Absence from a list that
	// is not Complete proves nothing.
	//
	// A repeated title makes the fetch incomplete because it is what two
	// overlapping pages look like: [A, B] then [B, D] of a list of four adds
	// up to four entries although C was never read. A list that really does
	// hold a title twice is therefore never Complete, and the sync never
	// removes anything for it. That is the safe side of a case that cannot be
	// told apart from a paging fault.
	Complete bool
}

// APIError is a response from IMDb that is not a usable page: a non-2xx
// status or a GraphQL error.
type APIError struct {
	StatusCode int    // HTTP status; 200 for a GraphQL error
	Code       string // GraphQL error code, if any (never the message)
	retryable  bool
	retryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return "imdb: graphql error " + e.Code
	}
	return "imdb: status " + strconv.Itoa(e.StatusCode)
}

// Client reads lists from IMDb. It makes one request at a time.
type Client struct {
	baseURL     string
	httpClient  *http.Client
	maxAttempts int
	maxPages    int
	sleep       func(ctx context.Context, d time.Duration) error
}

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL overrides the endpoint (used by tests).
func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = base }
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

// New returns a Client for IMDb's GraphQL endpoint.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL:     defaultBaseURL,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		maxAttempts: defaultMaxAttempts,
		maxPages:    defaultMaxPages,
		sleep:       sleepContext,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// The two queries differ in the root field and in the name of the field that
// holds a title: "item" on a Watchlist, "listItem" on a list. __typename
// tells a list entry that is not a title (a person) from one that could not
// be read.
const (
	watchlistQuery = `query($user: ID!, $after: ID) { predefinedList(classType: WATCH_LIST, userId: $user) { items(first: 250, after: $after) { total pageInfo { hasNextPage endCursor } edges { node { item { __typename ... on Title { id titleText { text } titleType { id isSeries } } } } } } } }`
	listQuery      = `query($id: ID!, $after: ID) { list(id: $id) { id items(first: 250, after: $after) { total pageInfo { hasNextPage endCursor } edges { node { listItem { __typename ... on Title { id titleText { text } titleType { id isSeries } } } } } } } }`
)

// source describes how to ask for one kind of list.
type source struct {
	query    string
	variable string // name of the query variable that takes the ID
	root     string // field under "data" that holds the list
}

func sourceFor(sourceID string) source {
	if strings.HasPrefix(sourceID, "ur") {
		return source{query: watchlistQuery, variable: "user", root: "predefinedList"}
	}
	return source{query: listQuery, variable: "id", root: "list"}
}

// FetchList reads a whole list, page by page. sourceID is a user ID (their
// Watchlist) or a list ID, as returned by ParseSource.
//
// A list that does not exist or is private is ErrNotFound. On any other
// error the returned List holds the entries read before the failure, with
// Complete false: they are real and may be added to a watchlist, but nothing
// may be concluded from what is not among them.
func (c *Client) FetchList(ctx context.Context, sourceID string) (List, error) {
	list := List{SourceID: sourceID}
	if !sourceIDPattern.MatchString(sourceID) {
		return list, ErrInvalidSource
	}
	src := sourceFor(sourceID)

	var (
		cursor       string
		unreadable   int
		totalChanged bool
		seenCursors  = map[string]bool{}
		seenTitles   = map[string]bool{}
	)
	for page := 1; ; page++ {
		if page > c.maxPages {
			return list, ErrTooLarge
		}
		items, err := c.fetchPage(ctx, src, sourceID, cursor)
		if err != nil {
			return list, err
		}
		if page > 1 && items.Total != list.Total {
			// The list changed while it was being read, so the pages do not
			// belong to one consistent view of it.
			totalChanged = true
		}
		list.Total = items.Total

		for _, edge := range items.Edges {
			node := edge.Node.entry()
			switch {
			case node != nil && ValidTitleID(node.ID) && seenTitles[node.ID]:
				list.Repeated++
			case node != nil && ValidTitleID(node.ID):
				seenTitles[node.ID] = true
				list.Entries = append(list.Entries, Entry{
					IMDbID:   node.ID,
					Title:    node.TitleText.Text,
					TypeID:   node.TitleType.ID,
					IsSeries: node.TitleType.IsSeries,
				})
			case node != nil && node.ID == "" && node.TypeName != "" && node.TypeName != "Title":
				list.Other++
			default:
				unreadable++
			}
		}

		if !items.PageInfo.HasNextPage {
			break
		}
		// A cursor that was handed out before, not only the one just used,
		// means the pages go round in a circle.
		next := items.PageInfo.EndCursor
		if next == "" || seenCursors[next] {
			return list, errors.New("imdb: paging did not advance")
		}
		seenCursors[next] = true
		cursor = next
	}

	list.Complete = !totalChanged && unreadable == 0 && list.Repeated == 0 &&
		len(list.Entries)+list.Other == list.Total
	return list, nil
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data   map[string]*listData `json:"data"`
	Errors []graphQLError       `json:"errors"`
}

type graphQLError struct {
	Extensions struct {
		Code        string `json:"code"`
		ErrorType   string `json:"errorType"`
		IsRetryable bool   `json:"isRetryable"`
	} `json:"extensions"`
}

type listData struct {
	Items *listItems `json:"items"`
}

type listItems struct {
	Total    int `json:"total"`
	PageInfo struct {
		HasNextPage bool   `json:"hasNextPage"`
		EndCursor   string `json:"endCursor"`
	} `json:"pageInfo"`
	Edges []struct {
		Node listNode `json:"node"`
	} `json:"edges"`
}

// listNode holds an entry under whichever name the query used.
type listNode struct {
	Item     *titleNode `json:"item"`
	ListItem *titleNode `json:"listItem"`
}

func (n listNode) entry() *titleNode {
	if n.Item != nil {
		return n.Item
	}
	return n.ListItem
}

type titleNode struct {
	TypeName  string `json:"__typename"`
	ID        string `json:"id"`
	TitleText struct {
		Text string `json:"text"`
	} `json:"titleText"`
	TitleType struct {
		ID       string `json:"id"`
		IsSeries bool   `json:"isSeries"`
	} `json:"titleType"`
}

// fetchPage requests one page, retrying what is worth retrying: network
// errors, rate limiting, server errors and GraphQL errors IMDb marks as
// retryable. Attempts are bounded and spaced out; this client would rather
// give up until tomorrow than lean on IMDb.
func (c *Client) fetchPage(ctx context.Context, src source, sourceID, cursor string) (*listItems, error) {
	variables := map[string]any{src.variable: sourceID, "after": nil}
	if cursor != "" {
		variables["after"] = cursor
	}
	body, err := json.Marshal(graphQLRequest{Query: src.query, Variables: variables})
	if err != nil {
		return nil, fmt.Errorf("imdb: encode request: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		items, err := c.fetchPageOnce(ctx, src, body)
		if err == nil {
			return items, nil
		}
		lastErr = err
		if ctx.Err() != nil || attempt == c.maxAttempts || !retryable(err) {
			return nil, err
		}
		wait := time.Duration(attempt) * time.Second
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.retryAfter > wait {
			wait = apiErr.retryAfter
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, lastErr
		}
	}
	return nil, lastErr
}

func retryable(err error) bool {
	if errors.Is(err, ErrNotFound) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.retryable
	}
	var decodeErr *decodeError
	return !errors.As(err, &decodeErr) // what is left are network errors
}

// decodeError is a 200 response that is not the JSON this client expects.
type decodeError struct{ err error }

func (e *decodeError) Error() string { return "imdb: decode response: " + e.err.Error() }
func (e *decodeError) Unwrap() error { return e.err }

func (c *Client) fetchPageOnce(ctx context.Context, src source, body []byte) (*listItems, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("imdb: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Imdb-Client-Name", clientName)
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Keep the cause, drop the URL that *url.Error repeats.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("imdb: request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
			retryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	var decoded graphQLResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&decoded); err != nil {
		return nil, &decodeError{err}
	}

	list := decoded.Data[src.root]
	if list == nil || list.Items == nil {
		// No list. A client-side error ("not found", "bad input", "not
		// allowed"), or no explanation at all, means the list does not exist
		// for us. Anything else is IMDb failing, which must not be reported
		// to the user as "make your list public".
		if len(decoded.Errors) == 0 || hasClientError(decoded.Errors) {
			return nil, ErrNotFound
		}
		return nil, graphQLFailure(decoded.Errors)
	}
	if len(decoded.Errors) > 0 {
		// Data and errors together: some field could not be resolved. The
		// page cannot be trusted to be whole, so it is not used.
		return nil, graphQLFailure(decoded.Errors)
	}
	return list.Items, nil
}

func hasClientError(errs []graphQLError) bool {
	for _, e := range errs {
		if strings.EqualFold(e.Extensions.ErrorType, "CLIENT") {
			return true
		}
	}
	return false
}

func graphQLFailure(errs []graphQLError) *APIError {
	apiErr := &APIError{StatusCode: http.StatusOK, Code: "UNKNOWN"}
	for _, e := range errs {
		if code := sanitizeCode(e.Extensions.Code); code != "" && apiErr.Code == "UNKNOWN" {
			apiErr.Code = code
		}
		if e.Extensions.IsRetryable {
			apiErr.retryable = true
		}
	}
	return apiErr
}

// sanitizeCode keeps a GraphQL error code only if it looks like one, so that
// nothing IMDb sends can put arbitrary text in an error message or a log.
func sanitizeCode(code string) string {
	if len(code) == 0 || len(code) > 64 {
		return ""
	}
	for _, r := range code {
		if !(r == '_' || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return ""
		}
	}
	return code
}

func parseRetryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
