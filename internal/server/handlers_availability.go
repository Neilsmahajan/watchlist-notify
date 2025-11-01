package server

import (
	"errors"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

// resolveRegion applies fallback: query > user.Region > TMDB_REGION > DEFAULT_REGION > "US"
func resolveRegion(c *gin.Context, user *models.User) string {
	r := strings.TrimSpace(c.Query("region"))
	if r == "" && user != nil && strings.TrimSpace(user.Region) != "" {
		r = user.Region
	}
	if r == "" {
		r = os.Getenv("TMDB_REGION")
	}
	if r == "" {
		r = os.Getenv("DEFAULT_REGION")
	}
	if r == "" {
		r = "US"
	}
	r = strings.ToUpper(r)
	if len(r) != 2 {
		r = "US"
	}
	return r
}

// availabilityHandler godoc
// @Summary Get streaming availability
// @Description Check which of the user's subscribed services have a specific movie or TV show available
// @Tags Availability
// @Security BearerAuth
// @Produce json
// @Param id path int true "TMDb content ID"
// @Param type query string true "Content type" Enums(movie, tv)
// @Param region query string false "ISO 3166-1 region code (defaults to user's region or US)"
// @Success 200 {object} map[string]interface{} "Availability details for user's services"
// @Failure 400 {object} ErrorResponse "Invalid ID or type"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 502 {object} ErrorResponse "Upstream TMDb API error"
// @Failure 503 {object} ErrorResponse "Availability service unavailable"
// @Router /api/availability/{id} [get]
func (s *Server) availabilityHandler(c *gin.Context) {
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, "availability unavailable")
		return
	}
	user, ok := s.getUser(c)
	if !ok {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if typ != "movie" && typ != "tv" {
		jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
		return
	}
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		jsonError(c, http.StatusBadRequest, "invalid id")
		return
	}
	region := resolveRegion(c, user)

	// Build quick lookup of user's active services
	active := map[string]bool{}
	for _, svc := range user.Services {
		if svc.Active {
			active[strings.ToLower(svc.Code)] = true
		}
	}

	// Call TMDb providers
	// Use the typed response and then select region
	var providerLink string
	type providerMatch struct {
		Code     string
		Name     string
		LogoPath string
		Access   map[string]bool
	}
	matches := map[string]*providerMatch{}
	var results map[string]tmdb.RegionProviders

	key := cache.ProvidersKey{
		ID:   id,
		Type: typ,
	}
	cacheEntry, err := s.cache.GetProvidersResults(c.Request.Context(), key)
	switch {
	case err == nil && cacheEntry != nil:
		results = cacheEntry.Results
	case errors.Is(err, cache.ErrMiss):
		fallthrough
	default:
		if err != nil && !errors.Is(err, cache.ErrMiss) {
			log.Printf("cache: providers lookup failed: %v", err)
		}
		resp, err := s.tmdb.GetProviders(c.Request.Context(), id, typ)
		if err != nil {
			jsonError(c, http.StatusBadGateway, "upstream providers failed")
			return
		}
		results = resp.Results
		if err := s.cache.SetProvidersResults(c.Request.Context(), key, cache.ProvidersValue{
			ID:      resp.ID,
			Results: results,
		}); err != nil {
			log.Printf("cache: providers set failed: %v", err)
		}
	}

	rp, ok := results[region]
	if !ok {
		// No providers for region
		c.JSON(http.StatusOK, gin.H{"region": region, "providers": []any{}, "unmatched_user_services": codesFromMap(active)})
		return
	}
	providerLink = rp.Link
	const (
		accessSubscription = models.ServiceAccessSubscription
		accessFree         = models.ServiceAccessFree
		accessAds          = models.ServiceAccessAds
	)
	collect := func(access string, list []tmdb.Provider) {
		for _, p := range list {
			code, ok := models.MapProviderNameToCode(p.ProviderName)
			if !ok {
				continue
			}
			code = strings.ToLower(code)
			if !active[code] {
				continue
			}
			match, exists := matches[code]
			if !exists {
				match = &providerMatch{
					Code:     code,
					Name:     displayNameForCode(code),
					LogoPath: p.LogoPath,
					Access:   map[string]bool{access: true},
				}
				matches[code] = match
				continue
			}
			if match.LogoPath == "" {
				match.LogoPath = p.LogoPath
			}
			match.Access[access] = true
		}
	}
	collect(accessSubscription, rp.Flatrate)
	collect(accessFree, rp.Free)
	collect(accessAds, rp.Ads)

	// Map to our service codes and intersect with user-active
	type outProv struct {
		Code     string   `json:"code"`
		Name     string   `json:"name"`
		LogoPath string   `json:"logo_path,omitempty"`
		Link     string   `json:"link,omitempty"`
		Access   []string `json:"access"`
	}

	accessOrder := []string{accessSubscription, accessFree, accessAds}
	var out []outProv
	for _, match := range matches {
		var ordered []string
		for _, key := range accessOrder {
			if match.Access[key] {
				ordered = append(ordered, key)
			}
		}
		if len(ordered) == 0 {
			ordered = append(ordered, accessSubscription)
		}
		out = append(out, outProv{
			Code:     match.Code,
			Name:     match.Name,
			LogoPath: match.LogoPath,
			Link:     providerLink,
			Access:   ordered,
		})
	}
	// Sort by display name for stability
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	// Compute unmatched user services (still active but not present for title)
	var unmatched []string
	for code, isActive := range active {
		if isActive && matches[code] == nil {
			unmatched = append(unmatched, code)
		}
	}
	sort.Strings(unmatched)

	payload := gin.H{
		"region":                  region,
		"providers":               out,
		"unmatched_user_services": unmatched,
	}

	c.JSON(http.StatusOK, payload)
}

func codesFromMap(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func displayNameForCode(code string) string {
	for _, s := range models.ServiceCatalog {
		if s.Code == code {
			return s.Name
		}
	}
	return code
}
