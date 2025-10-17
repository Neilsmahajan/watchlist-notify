package server

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
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

func (s *Server) importWatchlistHandler(c *gin.Context) {
	user, ok := s.getUser(c)
	if !ok {
		return
	}
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, "import unavailable")
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		jsonError(c, http.StatusBadRequest, "file is required")
		return
	}
	if fileHeader.Size == 0 {
		jsonError(c, http.StatusBadRequest, "file is empty")
		return
	}
	const maxImportSize = int64(10 << 20) // 10 MB
	if fileHeader.Size > maxImportSize {
		jsonError(c, http.StatusBadRequest, "file too large (max 10MB)")
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		jsonError(c, http.StatusBadRequest, "failed to open file")
		return
	}
	defer func(closer io.ReadCloser) {
		_ = closer.Close()
	}(file)

	result, err := s.importWatchlistFromCSV(c.Request.Context(), user.ID, file)
	if err != nil {
		jsonError(c, http.StatusBadRequest, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"imported":   result.Imported,
		"duplicates": result.Duplicates,
		"errors":     result.Errors,
	})
}

type watchlistImportResult struct {
	Imported   int                 `json:"imported"`
	Duplicates int                 `json:"duplicates"`
	Errors     []watchlistRowError `json:"errors"`
}

type watchlistRowError struct {
	Row    int    `json:"row"`
	Reason string `json:"reason"`
}

func (s *Server) importWatchlistFromCSV(ctx context.Context, userID primitive.ObjectID, reader io.Reader) (*watchlistImportResult, error) {
	if s.tmdb == nil {
		return nil, errors.New("tmdb client not configured")
	}

	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = -1
	csvReader.TrimLeadingSpace = true
	csvReader.LazyQuotes = true

	header, err := csvReader.Read()
	if err != nil {
		return nil, err
	}
	if len(header) == 0 {
		return nil, errors.New("missing header row")
	}
	header[0] = strings.TrimPrefix(header[0], "\ufeff")

	colIndex := make(map[string]int, len(header))
	for i, col := range header {
		colIndex[col] = i
	}
	constIdx, ok := colIndex["Const"]
	if !ok {
		return nil, errors.New("missing required column: Const")
	}

	result := &watchlistImportResult{}
	cache := make(map[string]*tmdb.Result)
	rowNumber := 1

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		rowNumber++
		if err != nil {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "failed to parse row"})
			continue
		}
		if len(record) <= constIdx {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "missing IMDb ID"})
			continue
		}

		imdbID := strings.TrimSpace(record[constIdx])
		if imdbID == "" {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "missing IMDb ID"})
			continue
		}

		tmdbItem, lookupErr := s.lookupTMDbByIMDb(ctx, imdbID, cache)
		if lookupErr != nil {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "tmdb lookup failed: " + lookupErr.Error()})
			continue
		}
		if tmdbItem == nil {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "no tmdb match for imdb id " + imdbID})
			continue
		}

		watchlistType, ok := tmdbMediaTypeToWatchlistType(tmdbItem.Type)
		if !ok {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "unsupported tmdb media type"})
			continue
		}

		title := strings.TrimSpace(tmdbItem.Title)
		if title == "" {
			title = imdbID
		}

		item := &models.WatchlistItem{
			UserID: userID,
			Title:  title,
			Type:   watchlistType,
			IMDbID: imdbID,
			Status: models.WatchlistStatusPlanned,
		}
		if tmdbItem.TMDBID > 0 {
			item.TMDbID = tmdbItem.TMDBID
		}
		if tmdbItem.Year > 0 {
			item.Year = tmdbItem.Year
		}

		if err := s.db.CreateWatchlistItem(ctx, item); err != nil {
			if errors.Is(err, database.ErrDuplicateWatchlistItem) {
				result.Duplicates++
				continue
			}
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: err.Error()})
			continue
		}

		result.Imported++
	}

	if result.Imported == 0 && result.Duplicates == 0 && len(result.Errors) > 0 {
		return nil, errors.New("no rows imported")
	}

	return result, nil
}

func (s *Server) lookupTMDbByIMDb(ctx context.Context, imdbID string, cache map[string]*tmdb.Result) (*tmdb.Result, error) {
	if cached, seen := cache[imdbID]; seen {
		return cached, nil
	}
	res, err := s.tmdb.FindByExternalID(ctx, imdbID, tmdb.ExternalSourceIMDbID)
	if err != nil {
		return nil, err
	}
	if res == nil {
		cache[imdbID] = nil
		return nil, nil
	}
	copy := *res
	cache[imdbID] = &copy
	return cache[imdbID], nil
}

func tmdbMediaTypeToWatchlistType(mediaType string) (string, bool) {
	switch strings.ToLower(mediaType) {
	case "movie":
		return models.WatchlistTypeMovie, true
	case "tv":
		return models.WatchlistTypeShow, true
	default:
		return "", false
	}
}
