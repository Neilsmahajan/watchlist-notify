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
	defaultBaseURL   = "https://api.themoviedb.org/3"
	defaultImageBase = "https://image.tmdb.org/t/p/w342"
)

// Client is a minimal TMDb v3 client using api_key query param auth.
type Client struct {
	apiKey     string
	bearer     string
	baseURL    string
	httpClient *http.Client
}

func New(apiKey string) (*Client, error) {
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
	return &Client{
		apiKey:  cred,
		bearer:  bearer,
		baseURL: strings.TrimRight(base, "/"),
		httpClient: &http.Client{
			Timeout: 7 * time.Second,
		},
	}, nil
}

type Result struct {
	TMDBID    int    `json:"tmdb_id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	Type      string `json:"type"` // movie | tv
	PosterURL string `json:"poster_url,omitempty"`
	Poster    string `json:"poster_path,omitempty"`
}

type searchResp struct {
	Page         int           `json:"page"`
	TotalPages   int           `json:"total_pages"`
	TotalResults int           `json:"total_results"`
	Results      []searchEntry `json:"results"`
}

type searchEntry struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
	PosterPath    string `json:"poster_path"`
	MediaType     string `json:"media_type"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
}

func (c *Client) SearchMovies(ctx context.Context, query string, page int, includeAdult bool, language, region string) ([]Result, int, int, error) {
	params := url.Values{}
	if c.apiKey != "" {
		params.Set("api_key", c.apiKey)
	}
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
	endpoint := fmt.Sprintf("%s/search/movie?%s", c.baseURL, params.Encode())
	return c.doSearch(ctx, endpoint, "movie")
}

func (c *Client) SearchTV(ctx context.Context, query string, page int, includeAdult bool, language string) ([]Result, int, int, error) {
	params := url.Values{}
	if c.apiKey != "" {
		params.Set("api_key", c.apiKey)
	}
	params.Set("query", query)
	params.Set("include_adult", strconv.FormatBool(includeAdult))
	if page > 0 {
		params.Set("page", strconv.Itoa(page))
	}
	if language != "" {
		params.Set("language", language)
	}
	endpoint := fmt.Sprintf("%s/search/tv?%s", c.baseURL, params.Encode())
	return c.doSearch(ctx, endpoint, "tv")
}

func (c *Client) doSearch(ctx context.Context, endpoint string, forcedType string) ([]Result, int, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println("Error closing response body:", err)
		}
	}(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, 0, fmt.Errorf("tmdb search failed: %d", resp.StatusCode)
	}
	var sr searchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, 0, 0, err
	}
	out := make([]Result, 0, len(sr.Results))
	for _, r := range sr.Results {
		title := r.Title
		if title == "" {
			title = r.Name
		}
		year := yearFromDate(r.ReleaseDate)
		if year == 0 {
			year = yearFromDate(r.FirstAirDate)
		}
		mtype := forcedType
		if mtype == "" {
			// fall back to media_type if available
			if r.MediaType == "movie" || r.MediaType == "tv" {
				mtype = r.MediaType
			}
		}
		posterURL := ""
		if r.PosterPath != "" {
			posterURL = defaultImageBase + r.PosterPath
		}
		out = append(out, Result{
			TMDBID:    r.ID,
			Title:     title,
			Year:      year,
			Type:      mtype,
			PosterURL: posterURL,
			Poster:    r.PosterPath,
		})
	}
	return out, sr.Page, sr.TotalPages, nil
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
	if s == "" {
		return false
	}
	if strings.Count(s, ".") >= 2 && len(s) > 50 {
		return true
	}
	return false
}

type Provider struct {
	ProviderID      int    `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	LogoPath        string `json:"logo_path"`
	DisplayPriority int    `json:"display_priority"`
}

type RegionProviders struct {
	Link     string     `json:"link"`
	Flatrate []Provider `json:"flatrate"`
	Free     []Provider `json:"free"`
	Ads      []Provider `json:"ads"`
	Buy      []Provider `json:"buy"`
	Rent     []Provider `json:"rent"`
}

type ProvidersResponse struct {
	ID      int                        `json:"id"`
	Results map[string]RegionProviders `json:"results"`
}

func (c *Client) GetMovieProviders(ctx context.Context, id int) (*ProvidersResponse, error) {
	endpoint := fmt.Sprintf("%s/movie/%d/watch/providers", c.baseURL, id)
	return c.doProviders(ctx, endpoint)
}

func (c *Client) GetTVProviders(ctx context.Context, id int) (*ProvidersResponse, error) {
	endpoint := fmt.Sprintf("%s/tv/%d/watch/providers", c.baseURL, id)
	return c.doProviders(ctx, endpoint)
}

func (c *Client) doProviders(ctx context.Context, endpoint string) (*ProvidersResponse, error) {
	// For providers, TMDb ignores api_key in query when using bearer, but we still attach api_key when present.
	// If apiKey is set and endpoint has no query, we add it for compatibility.
	if c.apiKey != "" {
		if !strings.Contains(endpoint, "?") {
			endpoint = endpoint + "?api_key=" + url.QueryEscape(c.apiKey)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println("Error closing response body:", err)
		}
	}(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tmdb providers failed: %d", resp.StatusCode)
	}
	var pr ProvidersResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	return &pr, nil
}
