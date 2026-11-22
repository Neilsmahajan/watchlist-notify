package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// createWatchlistItemHandler godoc
// @Summary Add item to watchlist
// @Description Create a new movie or TV show entry in the user's watchlist
// @Tags Watchlist
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body object{title=string,type=string,year=int,tmdb_id=int,imdb_id=string,status=string,tags=[]string} true "Watchlist item to create"
// @Success 201 {object} models.WatchlistItem "Created watchlist item"
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 409 {object} ErrorResponse "Duplicate item"
// @Failure 500 {object} ErrorResponse "Failed to create item"
// @Router /api/watchlist [post]
func (s *Server) createWatchlistItemHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	var body struct {
		Title  string   `json:"title" binding:"required"`
		Type   string   `json:"type"` // movie | show
		Year   int      `json:"year"`
		TMDB   *int     `json:"tmdb_id"`
		IMDB   string   `json:"imdb_id"`
		Status string   `json:"status"`
		Tags   []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}

	// Basic validation
	if body.Title == "" {
		jsonError(c, http.StatusBadRequest, "title required")
		return
	}
	typeVal := strings.ToLower(body.Type)
	if typeVal == "" {
		typeVal = models.WatchlistTypeMovie
	}
	if typeVal != models.WatchlistTypeMovie && typeVal != models.WatchlistTypeShow {
		jsonError(c, http.StatusBadRequest, "invalid type")
		return
	}
	statusVal := body.Status
	if statusVal == "" {
		statusVal = models.WatchlistStatusPlanned
	}
	if statusVal != models.WatchlistStatusPlanned && statusVal != models.WatchlistStatusWatching && statusVal != models.WatchlistStatusFinished {
		jsonError(c, http.StatusBadRequest, "invalid status")
		return
	}
	if body.Year != 0 {
		if body.Year < 1870 || body.Year > time.Now().Year()+1 { // basic sanity
			jsonError(c, http.StatusBadRequest, "invalid year")
			return
		}
	}

	item := &models.WatchlistItem{
		ID:        primitive.NilObjectID,
		UserID:    user.ID,
		Title:     body.Title,
		Type:      typeVal,
		Year:      body.Year,
		Tags:      body.Tags,
		Status:    statusVal,
		AddedAt:   time.Time{}, // set in DB layer
		UpdatedAt: time.Time{},
	}

	// Optional association with TMDb ID if provided
	if body.TMDB != nil {
		if *body.TMDB <= 0 {
			jsonError(c, http.StatusBadRequest, "invalid tmdb_id")
			return
		}
		item.TMDbID = *body.TMDB
	}

	if err := s.db.CreateWatchlistItem(c.Request.Context(), item); err != nil {
		if errors.Is(err, database.ErrDuplicateWatchlistItem) {
			jsonError(c, http.StatusConflict, "duplicate item")
			return
		}
		jsonError(c, http.StatusInternalServerError, "failed to create")
		return
	}

	// Fetch the title's availability now so the item shows it on the next
	// list. It is best effort: on a timeout or TMDb failure the list's miss
	// handling or the daily refresh fills it in.
	if ref, ok := item.TitleRef(); ok {
		s.ensureTitles(c.Request.Context(), user, []models.TitleRef{ref}, createEnsureBudget)
	}

	c.JSON(http.StatusCreated, item)
}

// createEnsureBudget bounds the availability fetch when adding one item.
const createEnsureBudget = 1500 * time.Millisecond

// ensureTitles stores availability baselines for titles that have none in the
// user's region, spending at most budget. Failures are logged by the
// refresher and otherwise ignored.
func (s *Server) ensureTitles(ctx context.Context, user *models.User, refs []models.TitleRef, budget time.Duration) {
	if len(refs) == 0 || budget <= 0 || !s.refresher.CanFetch() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	region := user.RegionOr(s.defaultRegion)
	keys := make([]string, len(refs))
	for i, ref := range refs {
		keys[i] = ref.Key()
	}
	stored, err := s.db.GetTitles(ctx, keys, []string{region})
	if err != nil {
		log.Printf("watchlist: loading titles before fetch failed: %v", err)
		return
	}
	s.refresher.Ensure(ctx, refs, stored, region)
}

// WatchlistListResponse is the body of GET /api/watchlist.
type WatchlistListResponse struct {
	Items []watchlist.Item  `json:"items"`
	Meta  WatchlistListMeta `json:"meta"`
}

// WatchlistListMeta describes the page and the list around it.
type WatchlistListMeta struct {
	Limit    int `json:"limit"`
	Offset   int `json:"offset"`
	Returned int `json:"returned"`
	// Total is the number of items matching the filters, before paging.
	Total int `json:"total"`
	// NextOffset is present when there are more items after this page.
	NextOffset *int `json:"next_offset,omitempty"`
	// Region is the region availability is reported for.
	Region string           `json:"region"`
	Facets watchlist.Facets `json:"facets"`
}

// listWatchlistItemsHandler godoc
// @Summary List watchlist items
// @Description Get the user's watchlist with title metadata and streaming availability attached to every item, with optional filtering, sorting and pagination. Availability comes from stored data: the request costs two database queries regardless of list size. The Server-Timing response header reports the database, TMDb and total durations.
// @Tags Watchlist
// @Security BearerAuth
// @Produce json
// @Param limit query int false "Number of items to return (0 for all, max 1000)" default(20)
// @Param offset query int false "Number of items to skip" default(0)
// @Param status query string false "Filter by status" Enums(planned, watching, finished)
// @Param type query string false "Filter by content type" Enums(movie, show)
// @Param search query string false "Case-insensitive search in title"
// @Param service query []string false "Keep items streaming on any of these service codes (repeat the parameter or separate with commas)" collectionFormat(multi)
// @Param available query boolean false "Keep items that are (true) or are not (false) streaming on one of the user's active services"
// @Param sort query string false "Sort field (prefix with - for descending): added_at, title, year, status, rating, runtime, available" default(-added_at)
// @Success 200 {object} WatchlistListResponse "Watchlist items with pagination metadata and facets"
// @Header 200 {string} Server-Timing "db, tmdb and total durations in milliseconds"
// @Failure 400 {object} ErrorResponse "Invalid query parameters"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 500 {object} ErrorResponse "Failed to fetch items"
// @Router /api/watchlist [get]
func (s *Server) listWatchlistItemsHandler(c *gin.Context) {
	start := time.Now()
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	q := c.Request.URL.Query()
	limit, offset, ok := parseLimitOffset(c)
	if !ok {
		return
	}
	opts := watchlist.Options{
		Status: strings.ToLower(q.Get("status")),
		Type:   strings.ToLower(q.Get("type")),
		Search: q.Get("search"),
		Sort:   q.Get("sort"), // e.g. -added_at, title, -rating
		Limit:  limit,
		Offset: offset,
	}
	if !validWatchlistStatus(opts.Status) {
		jsonError(c, http.StatusBadRequest, "invalid status filter")
		return
	}
	if !validWatchlistType(opts.Type) {
		jsonError(c, http.StatusBadRequest, "invalid type filter")
		return
	}
	if opts.Services, ok = parseServiceFilter(q["service"]); !ok {
		jsonError(c, http.StatusBadRequest, "invalid service filter")
		return
	}
	if opts.Available, ok = parseOptionalBool(q.Get("available")); !ok {
		jsonError(c, http.StatusBadRequest, "invalid available filter")
		return
	}

	view, err := s.lister.List(c.Request.Context(), user, opts)
	if err != nil {
		log.Printf("watchlist: list failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to fetch items")
		return
	}

	meta := WatchlistListMeta{
		Limit:    limit,
		Offset:   offset,
		Returned: len(view.Items),
		Total:    view.Total,
		Region:   view.Region,
		Facets:   view.Facets,
	}
	if next := offset + len(view.Items); next < view.Total {
		meta.NextOffset = &next
	}
	c.Header("Server-Timing", serverTiming(view.Timings, time.Since(start)))
	c.JSON(http.StatusOK, WatchlistListResponse{Items: view.Items, Meta: meta})
}

// parseServiceFilter reads service codes given as repeated parameters,
// comma-separated lists, or both. ok is false when a code is not a known
// service.
func parseServiceFilter(values []string) (codes []string, ok bool) {
	seen := make(map[string]bool)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			code := strings.ToLower(strings.TrimSpace(part))
			if code == "" || seen[code] {
				continue
			}
			if !models.ServiceCodeSet[code] {
				return nil, false
			}
			seen[code] = true
			codes = append(codes, code)
		}
	}
	return codes, true
}

// parseOptionalBool reads a tri-state query flag: absent, true or false.
func parseOptionalBool(value string) (*bool, bool) {
	var b bool
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return nil, true
	case "true", "1":
		b = true
	case "false", "0":
	default:
		return nil, false
	}
	return &b, true
}

// serverTiming formats a Server-Timing header value in milliseconds.
func serverTiming(t watchlist.Timings, total time.Duration) string {
	ms := func(d time.Duration) string {
		return strconv.FormatFloat(float64(d.Microseconds())/1000, 'f', 1, 64)
	}
	return "db;dur=" + ms(t.DB) + ", tmdb;dur=" + ms(t.TMDb) + ", total;dur=" + ms(total)
}

// updateWatchlistItemHandler godoc
// @Summary Update watchlist item
// @Description Update an existing watchlist item's details
// @Tags Watchlist
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param id path string true "Watchlist item ID (MongoDB ObjectID hex string)"
// @Param body body object{title=string,status=string,tags=[]string,year=int,tmdb_id=int} true "Fields to update"
// @Success 200 {object} models.WatchlistItem "Updated watchlist item"
// @Failure 400 {object} ErrorResponse "Invalid ID or request body"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 404 {object} ErrorResponse "Item not found"
// @Failure 409 {object} ErrorResponse "Duplicate item"
// @Failure 500 {object} ErrorResponse "Update failed"
// @Router /api/watchlist/{id} [patch]
func (s *Server) updateWatchlistItemHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	itemID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var body struct {
		Title  *string  `json:"title"`
		Status *string  `json:"status"`
		Tags   []string `json:"tags"`
		Year   *int     `json:"year"`
		TMDB   *int     `json:"tmdb_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	update := map[string]any{}
	if body.Title != nil {
		trimmed := strings.TrimSpace(*body.Title)
		if trimmed == "" {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "title cannot be empty"})
			return
		}
		update["title"] = trimmed
	}
	if body.Status != nil {
		st := *body.Status
		if st != models.WatchlistStatusPlanned && st != models.WatchlistStatusWatching && st != models.WatchlistStatusFinished {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
			return
		}
		update["status"] = st
	}
	if body.Tags != nil {
		update["tags"] = body.Tags
	}
	if body.Year != nil {
		if *body.Year < 1870 || *body.Year > time.Now().Year()+1 { // basic sanity
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid year"})
			return
		}
		update["year"] = *body.Year
	}
	if body.TMDB != nil {
		if *body.TMDB <= 0 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid tmdb_id"})
			return
		}
		update["tmdb_id"] = *body.TMDB
	}

	if len(update) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	updated, err := s.db.UpdateWatchlistItem(c.Request.Context(), user.ID, itemID, update)
	if err != nil {
		if errors.Is(err, database.ErrWatchlistItemNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if errors.Is(err, database.ErrDuplicateWatchlistItem) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "duplicate item"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
		return
	}

	c.JSON(http.StatusOK, updated)
}

// deleteWatchlistItemHandler godoc
// @Summary Delete watchlist item
// @Description Remove an item from the user's watchlist
// @Tags Watchlist
// @Security BearerAuth
// @Produce json
// @Param id path string true "Watchlist item ID (MongoDB ObjectID hex string)"
// @Success 200 {object} map[string]string "Deletion confirmation message"
// @Failure 400 {object} ErrorResponse "Invalid ID"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 404 {object} ErrorResponse "Item not found"
// @Failure 500 {object} ErrorResponse "Delete failed"
// @Router /api/watchlist/{id} [delete]
func (s *Server) deleteWatchlistItemHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	idStr := c.Param("id")
	itemID, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	err = s.db.DeleteWatchlistItem(c.Request.Context(), user.ID, itemID)
	if err != nil {
		if errors.Is(err, database.ErrWatchlistItemNotFound) {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "delete failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "watchlist item deleted"})
}
