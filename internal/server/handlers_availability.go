package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

const (
	// maxBatchSize caps one batch availability request.
	maxBatchSize = 500
	// batchMissBudget bounds how long a batch request waits for titles that
	// are not stored yet. Items still unresolved come back with no providers.
	batchMissBudget = 6 * time.Second
)

// AvailabilityResponse is the availability of one title on the user's services.
type AvailabilityResponse struct {
	Region                string         `json:"region"`
	Providers             []ProviderInfo `json:"providers"`
	UnmatchedUserServices []string       `json:"unmatched_user_services"`
}

// BatchAvailabilityRequest asks for the availability of several titles.
type BatchAvailabilityRequest struct {
	Items []struct {
		ID   int    `json:"id" binding:"required"`
		Type string `json:"type" binding:"required"` // "movie" or "tv"
	} `json:"items" binding:"required,dive"`
	Region string `json:"region"`
}

// BatchAvailabilityResponse maps "movie_123" / "tv_456" to availability.
type BatchAvailabilityResponse struct {
	Region  string                              `json:"region"`
	Results map[string]AvailabilityItemResponse `json:"results"`
}

// AvailabilityItemResponse is one title's entry in a batch response.
type AvailabilityItemResponse struct {
	Providers             []ProviderInfo `json:"providers"`
	UnmatchedUserServices []string       `json:"unmatched_user_services"`
}

// ProviderInfo is one of the user's services streaming a title.
type ProviderInfo struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	LogoPath string   `json:"logo_path,omitempty"`
	Link     string   `json:"link,omitempty"`
	Access   []string `json:"access"`
}

// resolveRegion picks the region for an availability lookup:
// query > user.Region > TMDB_REGION > the default region.
func (s *Server) resolveRegion(c *gin.Context, user *models.User) string {
	candidates := []string{c.Query("region")}
	if user != nil {
		candidates = append(candidates, user.Region)
	}
	candidates = append(candidates, os.Getenv("TMDB_REGION"))
	for _, candidate := range candidates {
		if region, ok := models.NormalizeRegion(candidate); ok {
			return region
		}
	}
	return s.defaultRegion
}

// regionIsStored reports whether lookups for region may be written to the
// title store. Only the regions the refresh job keeps current qualify:
// storing an arbitrary ?region= would leave snapshots that nothing refreshes.
func (s *Server) regionIsStored(user *models.User, region string) bool {
	return region == s.defaultRegion || region == user.RegionOr(s.defaultRegion)
}

// snapshotsFor returns the provider snapshot of each title in region, keyed
// by title key. Stored snapshots are used as they are; titles without one are
// fetched from TMDb until ctx ends and, for stored regions, saved as
// baselines. Titles that could not be resolved are absent from the result.
func (s *Server) snapshotsFor(ctx context.Context, user *models.User, refs []models.TitleRef, region string) (map[string]models.RegionSnapshot, error) {
	keys := make([]string, len(refs))
	for i, ref := range refs {
		keys[i] = ref.Key()
	}
	titles, err := s.db.GetTitles(ctx, keys, []string{region})
	if err != nil {
		return nil, err
	}

	snapshots := make(map[string]models.RegionSnapshot, len(refs))
	var missing []models.TitleRef
	for _, ref := range refs {
		title := titles[ref.Key()]
		if snap, ok := title.Snapshot(region); ok {
			snapshots[ref.Key()] = snap
		} else if title == nil || title.NotFoundAt == nil {
			missing = append(missing, ref)
		}
	}
	if len(missing) == 0 {
		return snapshots, nil
	}

	if s.regionIsStored(user, region) {
		for key, title := range s.refresher.Ensure(ctx, missing, titles, region) {
			if snap, ok := title.Snapshot(region); ok {
				snapshots[key] = snap
			}
		}
		return snapshots, nil
	}
	for key, snap := range s.refresher.PeekAll(ctx, missing, region) {
		snapshots[key] = snap
	}
	return snapshots, nil
}

// storedSnapshot reads one title's snapshot from the store with a short
// deadline of its own. It is the fallback for a lookup that ran out of time.
func (s *Server) storedSnapshot(ctx context.Context, ref models.TitleRef, region string) (models.RegionSnapshot, bool) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	titles, err := s.db.GetTitles(ctx, []string{ref.Key()}, []string{region})
	if err != nil {
		return models.RegionSnapshot{}, false
	}
	return titles[ref.Key()].Snapshot(region)
}

// providerInfos converts a user's view of a snapshot into the response shape
// these endpoints have always had.
func providerInfos(av *watchlist.Availability) []ProviderInfo {
	infos := make([]ProviderInfo, 0, len(av.Providers))
	for _, p := range av.Providers {
		infos = append(infos, ProviderInfo{
			Code:     p.Code,
			Name:     p.Name,
			LogoPath: p.LogoPath,
			Link:     av.Link,
			Access:   p.Access,
		})
	}
	return infos
}

// availabilityHandler godoc
// @Summary Get streaming availability
// @Description Check which of the user's subscribed services have a specific movie or TV show available. Served from stored availability (refreshed daily); TMDb is called only for a title that is not stored yet.
// @Tags Availability
// @Security BearerAuth
// @Produce json
// @Param id path int true "TMDb content ID"
// @Param type query string true "Content type" Enums(movie, tv)
// @Param region query string false "ISO 3166-1 region code (defaults to user's region or US)"
// @Success 200 {object} AvailabilityResponse "Availability details for user's services"
// @Failure 400 {object} ErrorResponse "Invalid ID or type"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 500 {object} ErrorResponse "Failed to load availability"
// @Failure 502 {object} ErrorResponse "Upstream TMDb API error"
// @Failure 503 {object} ErrorResponse "Availability service unavailable"
// @Failure 504 {object} ErrorResponse "Availability lookup timed out"
// @Router /api/availability/{id} [get]
func (s *Server) availabilityHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if typ != models.MediaTypeMovie && typ != models.MediaTypeTV {
		jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		jsonError(c, http.StatusBadRequest, "invalid id")
		return
	}
	region := s.resolveRegion(c, user)
	ref := models.TitleRef{MediaType: typ, TMDbID: id}
	active := user.ActiveServiceCodes()

	// One deadline for the whole lookup: the store read and, for a title that
	// is not stored, the TMDb fetch with its retries.
	ctx, cancel := s.lookupContext(c.Request.Context())
	defer cancel()

	titles, err := s.db.GetTitles(ctx, []string{ref.Key()}, []string{region})
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			jsonError(c, http.StatusGatewayTimeout, "availability lookup timed out")
			return
		}
		jsonError(c, http.StatusInternalServerError, "failed to load availability")
		return
	}
	title := titles[ref.Key()]
	snap, found := title.Snapshot(region)

	if !found && (title == nil || title.NotFoundAt == nil) {
		if !s.refresher.CanFetch() {
			jsonError(c, http.StatusServiceUnavailable, "availability unavailable")
			return
		}
		stored := s.regionIsStored(user, region)
		if stored {
			var res availability.Result
			if res, err = s.refresher.Refresh(ctx, ref, title, []string{region}); err == nil {
				snap, found = res.Title.Snapshot(region)
			}
		} else if snap, err = s.refresher.Peek(ctx, ref, region); err == nil {
			found = true
		}
		switch {
		case err == nil && !found:
			// The fetch worked but left no snapshot to answer from: another
			// request stored this title first and it could not be read back.
			// Look once more rather than report "available nowhere".
			if snap, found = s.storedSnapshot(c.Request.Context(), ref, region); !found {
				log.Printf("availability: no snapshot after refresh for %s", ref.Key())
				jsonError(c, http.StatusBadGateway, "upstream providers failed")
				return
			}
		case err == nil || errors.Is(err, availability.ErrTitleNotFound):
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			// Out of time. The title may have been stored meanwhile, by
			// another request or by this one's own write; answer from the
			// store if so, and say it timed out if not.
			if stored {
				snap, found = s.storedSnapshot(c.Request.Context(), ref, region)
			}
			if !found {
				log.Printf("availability: lookup timed out for %s", ref.Key())
				jsonError(c, http.StatusGatewayTimeout, "availability lookup timed out")
				return
			}
		default:
			log.Printf("availability: lookup failed for %s: %v", ref.Key(), err)
			jsonError(c, http.StatusBadGateway, "upstream providers failed")
			return
		}
	}

	// A title TMDb does not have is reported as available nowhere.
	var view *watchlist.Availability
	if found {
		view = watchlist.BuildAvailability(snap, region, active)
	} else {
		view = &watchlist.Availability{Region: region}
	}
	c.JSON(http.StatusOK, AvailabilityResponse{
		Region:                region,
		Providers:             providerInfos(view),
		UnmatchedUserServices: view.Unmatched(active),
	})
}

// batchAvailabilityHandler godoc
// @Summary Batch check streaming availability
// @Description Check availability for multiple movies/TV shows across user's subscribed services in a single request. Served from stored availability; prefer GET /api/watchlist, which already embeds availability for every item.
// @Tags Availability
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body BatchAvailabilityRequest true "List of items to check (max 500)"
// @Success 200 {object} BatchAvailabilityResponse "Batch availability results"
// @Failure 400 {object} ErrorResponse "Invalid request body or exceeds batch size limit"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 500 {object} ErrorResponse "Failed to load availability"
// @Router /api/availability/batch [post]
func (s *Server) batchAvailabilityHandler(c *gin.Context) {
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
	if len(req.Items) > maxBatchSize {
		jsonError(c, http.StatusBadRequest, "batch size exceeds maximum")
		return
	}

	refs := make([]models.TitleRef, 0, len(req.Items))
	seen := make(map[string]bool, len(req.Items))
	for _, item := range req.Items {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		if typ != models.MediaTypeMovie && typ != models.MediaTypeTV {
			jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
			return
		}
		if item.ID <= 0 {
			jsonError(c, http.StatusBadRequest, "invalid id")
			return
		}
		ref := models.TitleRef{MediaType: typ, TMDbID: item.ID}
		if !seen[ref.Key()] {
			seen[ref.Key()] = true
			refs = append(refs, ref)
		}
	}

	region := s.resolveRegion(c, user)
	if r, ok := models.NormalizeRegion(req.Region); ok {
		region = r
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), batchMissBudget)
	defer cancel()
	snapshots, err := s.snapshotsFor(ctx, user, refs, region)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "failed to load availability")
		return
	}

	active := user.ActiveServiceCodes()
	results := make(map[string]AvailabilityItemResponse, len(refs))
	for _, ref := range refs {
		view := &watchlist.Availability{Region: region}
		if snap, ok := snapshots[ref.Key()]; ok {
			view = watchlist.BuildAvailability(snap, region, active)
		}
		results[batchKey(ref)] = AvailabilityItemResponse{
			Providers:             providerInfos(view),
			UnmatchedUserServices: view.Unmatched(active),
		}
	}
	c.JSON(http.StatusOK, BatchAvailabilityResponse{Region: region, Results: results})
}

// batchKey is the key a title has in a batch response: "movie_123", "tv_456".
func batchKey(ref models.TitleRef) string {
	return ref.MediaType + "_" + strconv.Itoa(ref.TMDbID)
}
