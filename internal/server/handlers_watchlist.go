package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

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

	c.JSON(http.StatusCreated, item)
}

func (s *Server) listWatchlistItemsHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}

	// Parse query params
	q := c.Request.URL.Query()
	limit, offset, ok := parseLimitOffset(c)
	if !ok {
		return
	}
	status := strings.ToLower(q.Get("status"))
	if !validWatchlistStatus(status) {
		jsonError(c, http.StatusBadRequest, "invalid status filter")
		return
	}
	itemType := strings.ToLower(q.Get("type"))
	if !validWatchlistType(itemType) {
		jsonError(c, http.StatusBadRequest, "invalid type filter")
		return
	}
	search := q.Get("search")
	sort := q.Get("sort") // e.g. -added_at, title, -year
	withCount := q.Get("with_count") == "1"

	opts := models.ListWatchlistOptions{
		UserID: user.ID,
		Limit:  limit,
		Offset: offset,
		Status: status,
		Type:   itemType,
		Search: search,
		Sort:   sort,
	}
	items, err := s.db.ListWatchlistItems(c.Request.Context(), opts)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch items"})
		return
	}
	meta := gin.H{"limit": limit, "offset": offset, "returned": len(items)}
	if len(items) == limit {
		meta["next_offset"] = offset + limit
	}
	if withCount {
		if total, cntErr := s.db.CountWatchlistItems(c.Request.Context(), opts); cntErr == nil {
			meta["total"] = total
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "meta": meta})
}

func (s *Server) updateWatchlistItemHandler(c *gin.Context) {
	emailVal, ok := c.Get("user_email")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	email := emailVal.(string)

	user, err := s.db.GetUserByEmail(c.Request.Context(), email)
	if err != nil || user == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
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

func (s *Server) deleteWatchlistItemHandler(c *gin.Context) {
	emailVal, ok := c.Get("user_email")
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	email := emailVal.(string)

	user, err := s.db.GetUserByEmail(c.Request.Context(), email)
	if err != nil || user == nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
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
