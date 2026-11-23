package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
)

// healthHandler godoc
// @Summary Health check
// @Description Reports whether the API can reach its database. A failed check answers 503 and the process keeps running.
// @Tags System
// @Produce json
// @Success 200 {object} map[string]string "Service is healthy"
// @Failure 503 {object} map[string]string "Database unreachable"
// @Router /health [get]
func (s *Server) healthHandler(c *gin.Context) {
	if err := s.db.Health(c.Request.Context()); err != nil {
		log.Printf("health: database check failed: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "message": "database unreachable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "message": "It's healthy"})
}

// searchHandler proxies a minimal search to TMDb for movies or TV shows.
// Query params: query (required), type=movie|tv (default movie), page (default 1), include_adult (default false)
// searchHandler godoc
// @Summary Search movies and TV shows
// @Description Search for movies or TV shows via TMDb API with pagination and filters
// @Tags Content Discovery
// @Security BearerAuth
// @Produce json
// @Param query query string true "Search query string"
// @Param type query string false "Content type: movie or tv" default(movie) Enums(movie, tv)
// @Param page query int false "Page number for pagination (1-1000)" default(1) minimum(1) maximum(1000)
// @Param include_adult query boolean false "Include adult content in results" default(false)
// @Param language query string false "ISO 639-1 language code" default(en-US)
// @Param region query string false "ISO 3166-1 region code" default(US)
// @Success 200 {object} map[string]interface{} "Search results with pagination"
// @Failure 400 {object} ErrorResponse "Invalid query parameters"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 502 {object} ErrorResponse "Upstream TMDb API error"
// @Failure 503 {object} ErrorResponse "Search service unavailable"
// @Failure 504 {object} ErrorResponse "Upstream TMDb API timed out"
// @Router /api/search [get]
func (s *Server) searchHandler(c *gin.Context) {
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, errSearchUnavailable.Error())
		return
	}
	q := strings.TrimSpace(c.Query("query"))
	if q == "" {
		jsonError(c, http.StatusBadRequest, "query required")
		return
	}
	typ := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if typ == "" {
		typ = "movie"
	}
	if typ != "movie" && typ != "tv" {
		jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
		return
	}
	page := 1
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			page = n
		} else {
			jsonError(c, http.StatusBadRequest, "invalid page")
			return
		}
	}
	includeAdult := false
	if v := c.Query("include_adult"); v != "" {
		includeAdult = v == "1" || strings.EqualFold(v, "true")
	}
	key := cache.SearchKey{
		Query:        q,
		Page:         page,
		IncludeAdult: includeAdult,
		Language:     c.Query("language"),
		Region:       searchRegion(c.Query("region")),
		Type:         typ,
	}

	found, err := s.searchTitles(c.Request.Context(), key)
	switch {
	case errors.Is(err, errSearchUnavailable):
		jsonError(c, http.StatusServiceUnavailable, err.Error())
		return
	case errors.Is(err, errSearchTimedOut):
		jsonError(c, http.StatusGatewayTimeout, errSearchTimedOut.Error())
		return
	case err != nil:
		jsonError(c, http.StatusBadGateway, errSearchFailed.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"results":       found.Results,
		"page":          found.Page,
		"total_pages":   found.TotalPages,
		"query":         q,
		"type":          typ,
		"include_adult": includeAdult,
		"language":      key.Language,
		"region":        key.Region,
	})
}

// What searchTitles can fail with. The text is what a client is shown.
var (
	// errSearchUnavailable: the API runs without a TMDb key.
	errSearchUnavailable = errors.New("search unavailable")
	// errSearchTimedOut: TMDb did not answer within the lookup budget.
	errSearchTimedOut = errors.New("upstream search timed out")
	errSearchFailed   = errors.New("upstream search failed")
)

// searchRegion returns the region a search is made for: the one asked for,
// or the deployment's default.
func searchRegion(region string) string {
	if region == "" {
		return os.Getenv("TMDB_REGION")
	}
	return region
}

// searchTitles returns one page of TMDb search results, from the cache when
// it holds them and from TMDb, within the lookup budget, when it does not.
// It is what GET /api/search and the search_titles tool both do, so a search
// made by one is a cache hit for the other.
//
// A cache that cannot be read or written is logged and otherwise ignored.
// The error wraps errSearchUnavailable, errSearchTimedOut or errSearchFailed.
func (s *Server) searchTitles(ctx context.Context, key cache.SearchKey) (*cache.SearchValue, error) {
	if s.tmdb == nil {
		return nil, errSearchUnavailable
	}
	cached, err := s.cache.GetSearchResults(ctx, key)
	if err == nil && cached != nil {
		return cached, nil
	}
	if err != nil && !errors.Is(err, cache.ErrMiss) {
		log.Printf("cache: search lookup failed: %v", err)
	}

	searchCtx, cancel := s.lookupContext(ctx)
	results, page, totalPages, err := s.tmdb.SearchTMDb(searchCtx, key.Query, key.Page, key.IncludeAdult, key.Language, key.Region, key.Type)
	timedOut := errors.Is(searchCtx.Err(), context.DeadlineExceeded)
	cancel()
	if err != nil {
		if timedOut {
			return nil, fmt.Errorf("%w: %w", errSearchTimedOut, err)
		}
		return nil, fmt.Errorf("%w: %w", errSearchFailed, err)
	}

	found := &cache.SearchValue{Results: results, Page: page, TotalPages: totalPages}
	if cacheErr := s.cache.SetSearchResults(ctx, key, *found); cacheErr != nil {
		log.Printf("cache: search store failed: %v", cacheErr)
	}
	return found, nil
}
