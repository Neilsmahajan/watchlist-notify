package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const movieBody = `{
  "id": 603,
  "title": "The Matrix",
  "release_date": "1999-03-31",
  "runtime": 136,
  "vote_average": 8.2,
  "vote_count": 26000,
  "poster_path": "/matrix.jpg",
  "overview": "A hacker learns the truth.",
  "imdb_id": "tt0133093",
  "genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
  "external_ids": {"imdb_id": "tt0133093"},
  "watch/providers": {"results": {
    "US": {
      "link": "https://www.themoviedb.org/movie/603/watch?locale=US",
      "flatrate": [{"provider_id": 1899, "provider_name": "HBO Max", "logo_path": "/hbo.jpg", "display_priority": 1}],
      "rent": [{"provider_id": 2, "provider_name": "Apple TV Store", "logo_path": "/atv.jpg"}]
    },
    "GB": {"link": "https://example.test/gb", "ads": [{"provider_id": 73, "provider_name": "Tubi TV"}]}
  }}
}`

const showBody = `{
  "id": 1399,
  "name": "Game of Thrones",
  "first_air_date": "2011-04-17",
  "episode_run_time": [0, 60],
  "vote_average": 8.5,
  "genres": [{"name": "Drama"}],
  "external_ids": {"imdb_id": "tt0944947"},
  "watch/providers": {"results": {}}
}`

// newTestClient returns a client that talks to handler and records, instead
// of taking, the waits between retries.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *[]time.Duration) {
	t.Helper()
	t.Setenv("TMDB_BEARER_TOKEN", "")
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := New("test-key", WithBaseURL(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	var waits []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	return c, &waits
}

func TestGetTitleMovie(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/603" {
			t.Errorf("path = %q, want /movie/603", r.URL.Path)
		}
		if got := r.URL.Query().Get("append_to_response"); got != "external_ids,watch/providers" {
			t.Errorf("append_to_response = %q", got)
		}
		if got := r.URL.Query().Get("api_key"); got != "test-key" {
			t.Errorf("api_key = %q", got)
		}
		_, _ = w.Write([]byte(movieBody))
	})

	got, err := c.GetTitle(context.Background(), "movie", 603)
	if err != nil {
		t.Fatal(err)
	}
	want := &TitleDetails{
		ID:         603,
		MediaType:  "movie",
		Title:      "The Matrix",
		Year:       1999,
		PosterPath: "/matrix.jpg",
		IMDbID:     "tt0133093",
		Runtime:    136,
		Genres:     []string{"Action", "Science Fiction"},
		Rating:     8.2,
		VoteCount:  26000,
		Overview:   "A hacker learns the truth.",
		Providers: map[string]RegionProviders{
			"US": {
				Link:     "https://www.themoviedb.org/movie/603/watch?locale=US",
				Flatrate: []Provider{{ProviderID: 1899, ProviderName: "HBO Max", LogoPath: "/hbo.jpg", DisplayPriority: 1}},
				Rent:     []Provider{{ProviderID: 2, ProviderName: "Apple TV Store", LogoPath: "/atv.jpg"}},
			},
			"GB": {Link: "https://example.test/gb", Ads: []Provider{{ProviderID: 73, ProviderName: "Tubi TV"}}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetTitle() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestGetTitleShow(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/1399" {
			t.Errorf("path = %q, want /tv/1399", r.URL.Path)
		}
		_, _ = w.Write([]byte(showBody))
	})

	got, err := c.GetTitle(context.Background(), "tv", 1399)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Game of Thrones" || got.Year != 2011 || got.Runtime != 60 || got.IMDbID != "tt0944947" {
		t.Errorf("show details = %+v", got)
	}
	// An empty results object is a real answer: no providers anywhere.
	if got.Providers == nil || len(got.Providers) != 0 {
		t.Errorf("Providers = %#v, want an empty map", got.Providers)
	}
}

func TestGetTitleWithoutProviderSectionIsAnError(t *testing.T) {
	bodies := map[string]string{
		"section missing": `{"id": 603, "title": "The Matrix"}`,
		"results missing": `{"id": 603, "title": "The Matrix", "watch/providers": {}}`,
		"results null":    `{"id": 603, "title": "The Matrix", "watch/providers": {"results": null}}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) })
			if _, err := c.GetTitle(context.Background(), "movie", 603); !errors.Is(err, ErrNoProviderData) {
				t.Errorf("GetTitle() error = %v, want ErrNoProviderData", err)
			}
		})
	}
}

func TestGetTitleNotFound(t *testing.T) {
	var calls atomic.Int32
	c, waits := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, `{"status_code":34}`, http.StatusNotFound)
	})

	_, err := c.GetTitle(context.Background(), "movie", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTitle() error = %v, want ErrNotFound", err)
	}
	if calls.Load() != 1 || len(*waits) != 0 {
		t.Errorf("a 404 was retried: %d calls, waits %v", calls.Load(), *waits)
	}
}

func TestGetTitleRetriesRateLimit(t *testing.T) {
	var calls atomic.Int32
	c, waits := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable) // no Retry-After: backoff
		default:
			_, _ = w.Write([]byte(movieBody))
		}
	})

	got, err := c.GetTitle(context.Background(), "movie", 603)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "The Matrix" {
		t.Errorf("Title = %q after retries", got.Title)
	}
	if want := []time.Duration{2 * time.Second, time.Second}; !reflect.DeepEqual(*waits, want) {
		t.Errorf("waits = %v, want %v", *waits, want)
	}
}

func TestGetTitleGivesUpAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	c, waits := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600") // capped
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := c.GetTitle(context.Background(), "movie", 603)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("GetTitle() error = %v, want a 429 APIError", err)
	}
	if calls.Load() != defaultMaxAttempts {
		t.Errorf("made %d attempts, want %d", calls.Load(), defaultMaxAttempts)
	}
	for _, wait := range *waits {
		if wait != maxRetryWait {
			t.Errorf("wait = %v, want it capped at %v", wait, maxRetryWait)
		}
	}
}

func TestGetTitleStopsRetryingWhenContextEnds(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c.sleep = func(context.Context, time.Duration) error { return context.DeadlineExceeded }

	_, err := c.GetTitle(context.Background(), "movie", 603)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("GetTitle() error = %v, want the 429 that was being retried", err)
	}
	if calls.Load() != 1 {
		t.Errorf("made %d attempts after the context ended, want 1", calls.Load())
	}
}

func TestGetTitleDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	c, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := c.GetTitle(context.Background(), "movie", 603); err == nil {
		t.Fatal("GetTitle() succeeded on a 401")
	}
	if calls.Load() != 1 {
		t.Errorf("a 401 was retried %d times", calls.Load()-1)
	}
}

func TestGetTitleRejectsBadInput(t *testing.T) {
	c, _ := newTestClient(t, func(http.ResponseWriter, *http.Request) {
		t.Error("request sent for invalid input")
	})
	if _, err := c.GetTitle(context.Background(), "show", 1); err == nil {
		t.Error("GetTitle accepted media type \"show\"")
	}
	if _, err := c.GetTitle(context.Background(), "movie", 0); err == nil {
		t.Error("GetTitle accepted id 0")
	}
}

// The API key travels in the query string. A transport error must not carry
// it into logs.
func TestTransportErrorsDoNotLeakTheAPIKey(t *testing.T) {
	c, _ := newTestClient(t, func(http.ResponseWriter, *http.Request) {})
	c.baseURL = "http://127.0.0.1:1" // nothing listens here

	_, err := c.GetTitle(context.Background(), "movie", 603)
	if err == nil {
		t.Fatal("GetTitle() succeeded against a closed port")
	}
	if strings.Contains(err.Error(), "test-key") || strings.Contains(err.Error(), "api_key") {
		t.Errorf("error leaks the API key: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := map[string]time.Duration{
		"":        0,
		"5":       5 * time.Second,
		" 12 ":    12 * time.Second,
		"-1":      0,
		"garbage": 0,
	}
	for in, want := range tests {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got < 80*time.Second || got > 91*time.Second {
		t.Errorf("parseRetryAfter(HTTP date 90s ahead) = %v", got)
	}
}

func TestSearchReturnsWhatTellsTitlesApart(t *testing.T) {
	var gotPath, gotQuery string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"page": 1, "total_pages": 3, "results": [
			{"id": 1399, "name": "Game of Thrones", "first_air_date": "2011-04-17", "poster_path": "/got.jpg",
			 "overview": "  Seven noble families fight for control of Westeros.\n"},
			{"id": 2, "original_name": "Untitled"}
		]}`))
	})

	results, page, totalPages, err := c.SearchTMDb(context.Background(), "thrones", 1, false, "", "", "tv")
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/search/tv" || gotQuery != "thrones" || page != 1 || totalPages != 3 {
		t.Errorf("path = %q, query = %q, page = %d of %d", gotPath, gotQuery, page, totalPages)
	}
	want := []Result{
		{TMDBID: 1399, Title: "Game of Thrones", Year: 2011, Type: "tv", PosterURL: defaultImageBase + "/got.jpg",
			Poster: "/got.jpg", Overview: "Seven noble families fight for control of Westeros."},
		{TMDBID: 2, Title: "Untitled", Type: "tv"},
	}
	if !reflect.DeepEqual(results, want) {
		t.Errorf("results = %+v\nwant      %+v", results, want)
	}
}
