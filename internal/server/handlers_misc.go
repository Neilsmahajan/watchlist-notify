package server

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}

// searchHandler proxies a minimal search to TMDb for movies or TV shows.
// Query params: query (required), type=movie|tv (default movie), page (default 1), include_adult (default false)
func (s *Server) searchHandler(c *gin.Context) {
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, "search unavailable")
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
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 { // TMDb hard cap is high; keep sane
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
	language := c.Query("language")
	region := c.Query("region")
	if region == "" {
		if def := os.Getenv("TMDB_REGION"); def != "" {
			region = def
		}
	}

	var (
		results             []interface{}
		err                 error
		pageNow, totalPages int
	)
	if typ == "movie" {
		r, pg, tp, e := s.tmdb.SearchMovies(c.Request.Context(), q, page, includeAdult, language, region)
		err = e
		pageNow, totalPages = pg, tp
		// convert to []interface{} to avoid re-defining struct here; marshal identical
		results = make([]interface{}, len(r))
		for i := range r {
			results[i] = r[i]
		}
	} else {
		r, pg, tp, e := s.tmdb.SearchTV(c.Request.Context(), q, page, includeAdult, language)
		err = e
		pageNow, totalPages = pg, tp
		results = make([]interface{}, len(r))
		for i := range r {
			results[i] = r[i]
		}
	}
	if err != nil {
		// Hide upstream details
		jsonError(c, http.StatusBadGateway, "upstream search failed")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"results":       results,
		"page":          pageNow,
		"total_pages":   totalPages,
		"query":         q,
		"type":          typ,
		"include_adult": includeAdult,
		"language":      language,
		"region":        region,
	})
}
