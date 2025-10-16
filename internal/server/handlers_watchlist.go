package server

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
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
	// Strip BOM if present on first header column
	header[0] = strings.TrimPrefix(header[0], "\ufeff")

	colIndex := make(map[string]int, len(header))
	for i, col := range header {
		colIndex[col] = i
	}
	requiredCols := []string{"Title", "Title Type", "Const"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return nil, errors.New("missing required column: " + col)
		}
	}

	result := &watchlistImportResult{}
	rowNumber := 1 // header already read

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
		if len(record) == 0 {
			continue
		}

		title := strings.TrimSpace(record[colIndex["Title"]])
		if title == "" {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "missing title"})
			continue
		}

		imdbID := strings.TrimSpace(record[colIndex["Const"]])
		if imdbID == "" {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "missing IMDb ID"})
			continue
		}

		titleTypeRaw := strings.TrimSpace(record[colIndex["Title Type"]])
		typeValue, ok := normalizeTitleType(titleTypeRaw)
		if !ok {
			result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "unsupported title type: " + titleTypeRaw})
			continue
		}

		item := &models.WatchlistItem{
			UserID: userID,
			Title:  title,
			Type:   typeValue,
			IMDbID: imdbID,
			Status: models.WatchlistStatusPlanned,
		}

		if idx, ok := colIndex["Year"]; ok && idx < len(record) {
			yearStr := strings.TrimSpace(record[idx])
			if yearStr != "" {
				year, convErr := strconv.Atoi(yearStr)
				if convErr != nil {
					result.Errors = append(result.Errors, watchlistRowError{Row: rowNumber, Reason: "invalid year"})
					continue
				}
				item.Year = year
			}
		}

		if idx, ok := colIndex["Genres"]; ok && idx < len(record) {
			item.Tags = splitCSVList(record[idx])
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

func normalizeTitleType(raw string) (string, bool) {
	switch strings.ToLower(raw) {
	case "movie", "tv movie", "short", "video":
		return models.WatchlistTypeMovie, true
	case "tv series", "tv mini series", "tv special", "tv short", "tv episode":
		return models.WatchlistTypeShow, true
	default:
		return "", false
	}
}

func splitCSVList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
