// Package tmdb is a small client for the parts of the TMDb v3 API this
// service uses: search, find-by-external-id and title details with watch
// providers.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
)

const (
	defaultBaseURL       = "https://api.themoviedb.org/3"
	defaultImageBase     = "https://image.tmdb.org/t/p/w342"
	ExternalSourceIMDbID = "imdb_id"

	defaultMaxAttempts = 4
	maxRetryWait       = 10 * time.Second
)

// ErrNotFound is returned (wrapped in an *APIError) when TMDb answers 404.
var ErrNotFound = errors.New("tmdb: not found")

// ErrNoProviderData is returned when a title response arrives without its
// watch/providers section. That is a failed fetch, not "no providers".
var ErrNoProviderData = errors.New("tmdb: response has no watch provider data")

// APIError is a non-2xx response from TMDb.
type APIError struct {
	Op         string
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	return fmt.Sprintf("tmdb %s failed: status %d", e.Op, e.StatusCode)
}

// Is lets errors.Is(err, ErrNotFound) match a 404.
func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

func (e *APIError) retryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// Client is a minimal TMDb v3 client. It authenticates with either a v3 API
// key (query parameter) or a v4 bearer token.
type Client struct {
	apiKey      string
	bearer      string
	baseURL     string
	httpClient  *http.Client
	maxAttempts int
	sleep       func(ctx context.Context, d time.Duration) error
}

// Option customizes a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (used by tests).
func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(base, "/") }
}

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.httpClient = hc }
}

func New(apiKey string, opts ...Option) (*Client, error) {
	// Accept either a v3 API key (32-char) or a v4 bearer token (JWT-like),
	// also allow explicit TMDB_BEARER_TOKEN env to override.
	cred := strings.TrimSpace(apiKey)
	bearer := strings.TrimSpace(os.Getenv("TMDB_BEARER_TOKEN"))
	if bearer == "" && looksLikeBearer(cred) {
		bearer = cred
		cred = ""
	}
	if cred == "" && bearer == "" {
		return nil, errors.New("tmdb credentials missing (set TMDB_API_KEY for v3 or TMDB_BEARER_TOKEN for v4)")
	}

	// Support TMDB_API_BASE (original) and TMDB_BASE_URL (common naming) envs.
	base := os.Getenv("TMDB_API_BASE")
	if strings.TrimSpace(base) == "" {
		base = os.Getenv("TMDB_BASE_URL")
	}
	if strings.TrimSpace(base) == "" {
		base = defaultBaseURL
	}
	c := &Client{
		apiKey:      cred,
		bearer:      bearer,
		baseURL:     strings.TrimRight(base, "/"),
		httpClient:  &http.Client{Timeout: 7 * time.Second},
		maxAttempts: defaultMaxAttempts,
		sleep:       sleepContext,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

type Result struct {
	TMDBID    int    `json:"tmdb_id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Type      string `json:"type"` // movie | tv
	PosterURL string `json:"poster_url,omitempty"`
	Poster    string `json:"poster_path,omitempty"`
	// Overview is TMDb's synopsis. It tells apart titles that share a name.
	Overview string `json:"overview,omitempty"`
}

type SearchResp struct {
	Page         int           `json:"page"`
	TotalPages   int           `json:"total_pages"`
	TotalResults int           `json:"total_results"`
	Results      []SearchEntry `json:"results"`
}

type SearchEntry struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
	PosterPath    string `json:"poster_path"`
	MediaType     string `json:"media_type"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	Overview      string `json:"overview"`
}

type findResp struct {
	MovieResults []SearchEntry `json:"movie_results"`
	TVResults    []SearchEntry `json:"tv_results"`
}

func (c *Client) SearchTMDb(ctx context.Context, query string, page int, includeAdult bool, language, region, typ string) ([]Result, int, int, error) {
	params := url.Values{}
	params.Set("query", query)
	params.Set("include_adult", strconv.FormatBool(includeAdult))
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
	if language != "" {
		params.Set("language", language)
	}
	if region != "" {
		params.Set("region", region)
	}
	if typ != "movie" && typ != "tv" {
		typ = "movie"
	}
	var sr SearchResp
	if err := c.getJSON(ctx, "search", "/search/"+typ, params, &sr); err != nil {
		return nil, 0, 0, err
	}
	out := make([]Result, 0, len(sr.Results))
	for _, r := range sr.Results {
		out = append(out, entryToResult(r, typ))
	}
	return out, sr.Page, sr.TotalPages, nil
}

func (c *Client) FindByExternalID(ctx context.Context, externalID, source string) (*Result, error) {
	if externalID == "" {
		return nil, errors.New("external id is required")
	}
	if source == "" {
		return nil, errors.New("source is required")
	}
	params := url.Values{}
	params.Set("external_source", source)
	var fr findResp
	if err := c.getJSON(ctx, "find by external id", "/find/"+url.PathEscape(externalID), params, &fr); err != nil {
		return nil, err
	}
	if len(fr.MovieResults) > 0 {
		res := entryToResult(fr.MovieResults[0], "movie")
		return &res, nil
	}
	if len(fr.TVResults) > 0 {
		res := entryToResult(fr.TVResults[0], "tv")
		return &res, nil
	}
	return nil, nil
}

// Provider is one entry in a TMDb watch-provider list.
type Provider struct {
	ProviderID      int    `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	LogoPath        string `json:"logo_path"`
	DisplayPriority int    `json:"display_priority"`
}

// RegionProviders is the watch-provider data for a title in one region.
type RegionProviders struct {
	Link     string     `json:"link"`
	Flatrate []Provider `json:"flatrate"`
	Buy      []Provider `json:"buy"`
	Rent     []Provider `json:"rent"`
	Free     []Provider `json:"free"`
	Ads      []Provider `json:"ads"`
}

// TitleDetails is a title's metadata together with its watch providers in
// every region TMDb knows about.
type TitleDetails struct {
	ID         int
	MediaType  string // movie | tv
	Title      string
	Year       int
	PosterPath string
	IMDbID     string
	Runtime    int // minutes; for TV, the typical episode runtime
	Genres     []string
	Rating     float64
	VoteCount  int
	Overview   string
	// Providers is keyed by region. A region that is absent has no providers.
	Providers map[string]RegionProviders
}

type titleResp struct {
	ID             int     `json:"id"`
	Title          string  `json:"title"`
	Name           string  `json:"name"`
	OriginalTitle  string  `json:"original_title"`
	OriginalName   string  `json:"original_name"`
	ReleaseDate    string  `json:"release_date"`
	FirstAirDate   string  `json:"first_air_date"`
	Runtime        int     `json:"runtime"`
	EpisodeRunTime []int   `json:"episode_run_time"`
	VoteAverage    float64 `json:"vote_average"`
	VoteCount      int     `json:"vote_count"`
	PosterPath     string  `json:"poster_path"`
	Overview       string  `json:"overview"`
	IMDbID         string  `json:"imdb_id"`
	Genres         []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ExternalIDs *struct {
		IMDbID string `json:"imdb_id"`
	} `json:"external_ids"`
	WatchProviders *struct {
		Results map[string]RegionProviders `json:"results"`
	} `json:"watch/providers"`
}

// GetTitle fetches a title's details, external IDs and watch providers in a
// single request. mediaType is "movie" or "tv".
func (c *Client) GetTitle(ctx context.Context, mediaType string, id int) (*TitleDetails, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("tmdb: invalid media type %q", mediaType)
	}
	if id <= 0 {
		return nil, fmt.Errorf("tmdb: invalid id %d", id)
	}
	params := url.Values{}
	params.Set("append_to_response", "external_ids,watch/providers")

	var tr titleResp
	if err := c.getJSON(ctx, "title", fmt.Sprintf("/%s/%d", mediaType, id), params, &tr); err != nil {
		return nil, err
	}
	if tr.WatchProviders == nil || tr.WatchProviders.Results == nil {
		return nil, ErrNoProviderData
	}

	details := &TitleDetails{
		ID:         id,
		MediaType:  mediaType,
		Title:      firstNonEmpty(tr.Title, tr.Name, tr.OriginalTitle, tr.OriginalName),
		Year:       yearFromDate(tr.ReleaseDate),
		PosterPath: tr.PosterPath,
		IMDbID:     tr.IMDbID,
		Runtime:    tr.Runtime,
		Rating:     tr.VoteAverage,
		VoteCount:  tr.VoteCount,
		Overview:   tr.Overview,
		Providers:  tr.WatchProviders.Results,
	}
	if details.Year == 0 {
		details.Year = yearFromDate(tr.FirstAirDate)
	}
	if details.IMDbID == "" && tr.ExternalIDs != nil {
		details.IMDbID = tr.ExternalIDs.IMDbID
	}
	if details.Runtime == 0 {
		for _, minutes := range tr.EpisodeRunTime {
			if minutes > 0 {
				details.Runtime = minutes
				break
			}
		}
	}
	for _, g := range tr.Genres {
		if name := strings.TrimSpace(g.Name); name != "" {
			details.Genres = append(details.Genres, name)
		}
	}
	return details, nil
}

// getJSON performs a GET and decodes the JSON body into out. Rate limiting
// (429) and transient gateway errors are retried with backoff, honoring
// Retry-After, until the context ends or attempts run out.
func (c *Client) getJSON(ctx context.Context, op, path string, params url.Values, out any) error {
	if params == nil {
		params = url.Values{}
	}
	if c.apiKey != "" {
		params.Set("api_key", c.apiKey)
	}
	endpoint := c.baseURL + path + "?" + params.Encode()

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		err := c.getOnce(ctx, op, endpoint, out)
		if err == nil {
			return nil
		}
		lastErr = err

		var apiErr *APIError
		if !errors.As(err, &apiErr) || !apiErr.retryable() || attempt == c.maxAttempts {
			return err
		}
		wait := apiErr.RetryAfter
		if wait <= 0 {
			wait = time.Duration(attempt) * 500 * time.Millisecond
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		if err := c.sleep(ctx, wait); err != nil {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) getOnce(ctx context.Context, op, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("tmdb %s: build request: %w", op, err)
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// *url.Error embeds the request URL, which carries the API key. Keep
		// the cause, drop the URL.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("tmdb %s: %w", op, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Op: op, StatusCode: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("tmdb %s: decode response: %w", op, err)
	}
	return nil
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
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

func entryToResult(r SearchEntry, typ string) Result {
	title := firstNonEmpty(r.Title, r.Name, r.OriginalTitle, r.OriginalName)
	year := yearFromDate(r.ReleaseDate)
	if year == 0 {
		year = yearFromDate(r.FirstAirDate)
	}
	mediaType := typ
	if mediaType == "" {
		mt := strings.ToLower(strings.TrimSpace(r.MediaType))
		if mt == "movie" || mt == "tv" {
			mediaType = mt
		}
	}
	if mediaType == "" {
		if r.FirstAirDate != "" {
			mediaType = "tv"
		} else {
			mediaType = "movie"
		}
	}
	posterURL := ""
	if r.PosterPath != "" {
		posterURL = defaultImageBase + r.PosterPath
	}
	return Result{
		TMDBID:    r.ID,
		Title:     title,
		Year:      year,
		Type:      mediaType,
		PosterURL: posterURL,
		Poster:    r.PosterPath,
		Overview:  strings.TrimSpace(r.Overview),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func yearFromDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}

func looksLikeBearer(s string) bool {
	// TMDb v4 tokens are long JWT-like strings with at least two dots.
	return strings.Count(s, ".") >= 2 && len(s) > 50
}
