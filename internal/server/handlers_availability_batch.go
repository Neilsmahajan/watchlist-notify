package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

type BatchAvailabilityRequest struct {
	Items []struct {
		ID   int    `json:"id" binding:"required"`
		Type string `json:"type" binding:"required"` // "movie" or "tv"
	} `json:"items" binding:"required,dive"`
	Region string `json:"region"`
}

type BatchAvailabilityResponse struct {
	Region  string                              `json:"region"`
	Results map[string]AvailabilityItemResponse `json:"results"` // key: "movie_123" or "tv_456"
}

type AvailabilityItemResponse struct {
	Providers             []ProviderInfo `json:"providers"`
	UnmatchedUserServices []string       `json:"unmatched_user_services"`
}

type ProviderInfo struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	LogoPath string   `json:"logo_path,omitempty"`
	Link     string   `json:"link,omitempty"`
	Access   []string `json:"access"`
}

// batchAvailabilityHandler godoc
// @Summary Batch check streaming availability
// @Description Check availability for multiple movies/TV shows across user's subscribed services in a single request
// @Tags Availability
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body BatchAvailabilityRequest true "List of items to check (max 500)"
// @Success 200 {object} BatchAvailabilityResponse "Batch availability results"
// @Failure 400 {object} ErrorResponse "Invalid request body or exceeds batch size limit"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 503 {object} ErrorResponse "Availability service unavailable"
// @Router /api/availability/batch [post]
func (s *Server) batchAvailabilityHandler(c *gin.Context) {
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, "availability unavailable")
		return
	}

	user, ok := s.getUser(c)
	if !ok {
		return
	}

	var req BatchAvailabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid request body")
		return
	}

	if len(req.Items) == 0 {
		jsonError(c, http.StatusBadRequest, "items array cannot be empty")
		return
	}

	// Limit batch size to prevent abuse
	const maxBatchSize = 500
	if len(req.Items) > maxBatchSize {
		jsonError(c, http.StatusBadRequest, "batch size exceeds maximum")
		return
	}

	// Validate types
	for _, item := range req.Items {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		if typ != "movie" && typ != "tv" {
			jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
			return
		}
		if item.ID <= 0 {
			jsonError(c, http.StatusBadRequest, "invalid id")
			return
		}
	}

	region := resolveRegion(c, user)
	if req.Region != "" {
		r := strings.ToUpper(strings.TrimSpace(req.Region))
		if len(r) == 2 {
			region = r
		}
	}

	// Build quick lookup of user's active services
	activeServices := map[string]bool{}
	for _, svc := range user.Services {
		if svc.Active {
			activeServices[strings.ToLower(svc.Code)] = true
		}
	}

	// Process items in parallel
	results := make(map[string]AvailabilityItemResponse)
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Use a semaphore to limit concurrent TMDb API calls
	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)

	for _, item := range req.Items {
		wg.Add(1)
		go func(id int, typ string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			key := s.makeItemKey(id, typ)
			result := s.fetchItemAvailability(c.Request.Context(), id, typ, region, activeServices)

			mu.Lock()
			results[key] = result
			mu.Unlock()
		}(item.ID, strings.ToLower(item.Type))
	}

	wg.Wait()

	response := BatchAvailabilityResponse{
		Region:  region,
		Results: results,
	}

	c.JSON(http.StatusOK, response)
}

func (s *Server) makeItemKey(id int, typ string) string {
	return typ + "_" + strconv.Itoa(id) // e.g., "movie_123" or "tv_456"
}

func (s *Server) fetchItemAvailability(
	ctx context.Context,
	id int,
	typ string,
	region string,
	activeServices map[string]bool,
) AvailabilityItemResponse {
	const (
		accessSubscription = models.ServiceAccessSubscription
		accessFree         = models.ServiceAccessFree
		accessAds          = models.ServiceAccessAds
	)

	type providerMatch struct {
		Code     string
		Name     string
		LogoPath string
		Access   map[string]bool
	}
	matches := map[string]*providerMatch{}
	var providerLink string
	var results map[string]tmdb.RegionProviders

	// Try cache first
	key := cache.ProvidersKey{
		ID:   id,
		Type: typ,
	}
	cacheEntry, err := s.cache.GetProvidersResults(ctx, key)
	switch {
	case err == nil && cacheEntry != nil:
		results = cacheEntry.Results
	case errors.Is(err, cache.ErrMiss):
		fallthrough
	default:
		if err != nil && !errors.Is(err, cache.ErrMiss) {
			log.Printf("cache: providers lookup failed for %s_%d: %v", typ, id, err)
		}
		resp, err := s.tmdb.GetProviders(ctx, id, typ)
		if err != nil {
			log.Printf("tmdb: providers fetch failed for %s_%d: %v", typ, id, err)
			return AvailabilityItemResponse{
				Providers:             []ProviderInfo{},
				UnmatchedUserServices: codesFromMap(activeServices),
			}
		}
		results = resp.Results
		if err := s.cache.SetProvidersResults(ctx, key, cache.ProvidersValue{
			ID:      resp.ID,
			Results: results,
		}); err != nil {
			log.Printf("cache: providers set failed for %s_%d: %v", typ, id, err)
		}
	}

	rp, ok := results[region]
	if !ok {
		// No providers for region
		return AvailabilityItemResponse{
			Providers:             []ProviderInfo{},
			UnmatchedUserServices: codesFromMap(activeServices),
		}
	}
	providerLink = rp.Link

	collect := func(access string, list []tmdb.Provider) {
		for _, p := range list {
			code, ok := models.MapProviderNameToCode(p.ProviderName)
			if !ok {
				continue
			}
			code = strings.ToLower(code)
			if !activeServices[code] {
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

	accessOrder := []string{accessSubscription, accessFree, accessAds}
	var providers []ProviderInfo
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
		providers = append(providers, ProviderInfo{
			Code:     match.Code,
			Name:     match.Name,
			LogoPath: match.LogoPath,
			Link:     providerLink,
			Access:   ordered,
		})
	}

	// Sort by display name for stability
	sort.Slice(providers, func(i, j int) bool { return providers[i].Name < providers[j].Name })

	// Compute unmatched user services
	var unmatched []string
	for code, isActive := range activeServices {
		if isActive && matches[code] == nil {
			unmatched = append(unmatched, code)
		}
	}
	sort.Strings(unmatched)

	return AvailabilityItemResponse{
		Providers:             providers,
		UnmatchedUserServices: unmatched,
	}
}
