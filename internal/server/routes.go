package server

import (
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/auth"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.Default()

	frontendURL := os.Getenv("FRONTEND_URL")

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*", frontendURL}, // Add your frontend URL
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true, // Enable cookies/auth
	}))

	r.GET("/health", s.healthHandler)
	r.GET("/auth/google/login", auth.LoginHandler)
	r.GET("/auth/google/callback", func(c *gin.Context) { auth.CallbackHandler(c, s.db) })
	r.POST("/auth/logout", func(c *gin.Context) { auth.LogoutHandler(c) })

	protected := r.Group("/api")
	protected.Use(middleware.AuthRequired())
	protected.GET("/me", s.meHandler)
	protected.PATCH("/me/preferences", s.updateUserPreferencesHandler)
	protected.PATCH("/me/services", s.updateUserServicesHandler)
	protected.GET("/services", s.listUserServicesHandler)
	protected.POST("/watchlist", s.createWatchlistItemHandler)
	protected.GET("/watchlist", s.listWatchlistItemsHandler)
	protected.PATCH("/watchlist/:id", s.updateWatchlistItemHandler)
	protected.DELETE("/watchlist/:id", s.deleteWatchlistItemHandler)
	return r
}

func (s *Server) HelloWorldHandler(c *gin.Context) {
	resp := make(map[string]string)
	resp["message"] = "Hello World"

	c.JSON(http.StatusOK, resp)
}

func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}

// meHandler returns the authenticated user's full record from the database
func (s *Server) meHandler(c *gin.Context) {
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
	c.JSON(http.StatusOK, user)
}

func (s *Server) updateUserPreferencesHandler(c *gin.Context) {
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

	var body struct {
		NotifyEmail      *string `json:"notify_email"`
		UseAccountEmail  *bool   `json:"use_account_email"`
		MarketingConsent *bool   `json:"marketing_consent"`
		DigestConsent    *bool   `json:"digest_consent"`
		DigestFrequency  *string `json:"digest_frequency"`
		QuietHoursStart  *int    `json:"quiet_hours_start"`
		QuietHoursEnd    *int    `json:"quiet_hours_end"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	// Build updates and validate
	updates := map[string]any{}
	if body.NotifyEmail != nil {
		email := strings.TrimSpace(*body.NotifyEmail)
		// allow empty if UseAccountEmail will be true
		if email == "" && (body.UseAccountEmail == nil || !*body.UseAccountEmail) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "notify_email cannot be empty unless use_account_email is true"})
			return
		}
		updates["preferences.notify_email"] = email
	}
	if body.UseAccountEmail != nil {
		updates["preferences.use_account_email"] = *body.UseAccountEmail
		// If switching to account email, we can clear notify_email to avoid confusion (optional)
		if *body.UseAccountEmail {
			// Only clear if caller didn’t specify a custom notify_email in same request
			if body.NotifyEmail == nil {
				updates["preferences.notify_email"] = ""
			}
		}
	}
	if body.MarketingConsent != nil {
		updates["preferences.marketing_consent"] = *body.MarketingConsent
	}
	if body.DigestConsent != nil {
		updates["preferences.digest_consent"] = *body.DigestConsent
	}
	if body.DigestFrequency != nil {
		df := strings.ToLower(strings.TrimSpace(*body.DigestFrequency))
		switch df {
		case models.DigestFrequencyDaily, models.DigestFrequencyWeekly, models.DigestFrequencyManual:
			updates["preferences.digest_frequency"] = df
		default:
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid digest_frequency"})
			return
		}
	}
	if body.QuietHoursStart != nil {
		if *body.QuietHoursStart < 0 || *body.QuietHoursStart > 23 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "quiet_hours_start must be 0-23"})
			return
		}
		updates["preferences.quiet_hours_start"] = *body.QuietHoursStart
	}
	if body.QuietHoursEnd != nil {
		if *body.QuietHoursEnd < 0 || *body.QuietHoursEnd > 23 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "quiet_hours_end must be 0-23"})
			return
		}
		updates["preferences.quiet_hours_end"] = *body.QuietHoursEnd
	}
	if len(updates) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "no fields to update"})
		return
	}

	updated, err := s.db.UpdateUserPreferences(c.Request.Context(), user.ID, updates)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to update preferences"})
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) updateUserServicesHandler(c *gin.Context) {
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

	type toggle struct {
		Code   string `json:"code" binding:"required"`   // e.g. "netflix"
		Active bool   `json:"active" binding:"required"` // true to add/activate, false to remove/deactivate
	}

	var body struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
		Toggle []toggle `json:"toggle"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	// Validate service codes and normalize intent into add/remove sets
	validCodes := models.ServiceCodeSet
	toAdd := map[string]bool{}
	toRemove := map[string]bool{}
	for _, code := range body.Add {
		code = strings.ToLower(code)
		if !validCodes[code] {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid service code in add: " + code})
			return
		}
		toAdd[code] = true
		delete(toRemove, code)
	}
	for _, code := range body.Remove {
		code = strings.ToLower(code)
		if !validCodes[code] {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid service code in remove: " + code})
			return
		}
		toRemove[code] = true
		delete(toAdd, code)
	}
	for _, t := range body.Toggle {
		code := strings.ToLower(t.Code)
		if !validCodes[code] {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid service code in toggle: " + code})
			return
		}
		if t.Active {
			toAdd[code] = true
			delete(toRemove, code)
		} else {
			toRemove[code] = true
			delete(toAdd, code)
		}
	}
	if len(toAdd) == 0 && len(toRemove) == 0 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "no changes requested"})
		return
	}

	// Merge with existing services without dropping unchanged entries
	now := time.Now()
	existing := map[string]models.ServiceSubscription{}
	for _, ssub := range user.Services {
		existing[strings.ToLower(ssub.Code)] = ssub
	}
	// Apply additions/activations
	for code := range toAdd {
		if es, ok := existing[code]; ok {
			es.Active = true
			if es.AddedAt.IsZero() {
				es.AddedAt = now
			}
			es.Code = code
			existing[code] = es
		} else {
			existing[code] = models.ServiceSubscription{Code: code, AddedAt: now, Active: true}
		}
	}
	// Apply removals/deactivations
	for code := range toRemove {
		if es, ok := existing[code]; ok {
			es.Active = false
			existing[code] = es
		}
		// If not already present, no-op; we don't create new inactive entries on remove
	}
	// Build slice back preserving all entries
	newServices := make([]models.ServiceSubscription, 0, len(existing))
	for _, v := range existing {
		newServices = append(newServices, v)
	}
	user.Services = newServices
	updatedUser, err := s.db.UpdateServices(c.Request.Context(), user.ID, user.Services)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to update services"})
		return
	}
	c.JSON(http.StatusOK, updatedUser)
}

func (s *Server) listUserServicesHandler(c *gin.Context) {
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

	// Map user's services by normalized code for quick lookup
	byCode := make(map[string]models.ServiceSubscription, len(user.Services))
	for _, ssub := range user.Services {
		byCode[strings.ToLower(ssub.Code)] = ssub
	}

	// Supported catalog
	catalog := models.ServiceCatalog

	type serviceOut struct {
		Code    string     `json:"code"`
		Name    string     `json:"name"`
		Active  bool       `json:"active"`
		AddedAt *time.Time `json:"added_at,omitempty"`
		Plan    string     `json:"plan,omitempty"`
	}

	out := make([]serviceOut, 0, len(catalog))
	for _, def := range catalog {
		entry, ok := byCode[def.Code]
		var added *time.Time
		if ok && !entry.AddedAt.IsZero() {
			t := entry.AddedAt
			added = &t
		}
		plan := ""
		if ok && entry.Plan != "" {
			plan = entry.Plan
		}
		out = append(out, serviceOut{
			Code:    def.Code,
			Name:    def.Name,
			Active:  ok && entry.Active,
			AddedAt: added,
			Plan:    plan,
		})
	}

	c.JSON(http.StatusOK, gin.H{"services": out})
}

func (s *Server) createWatchlistItemHandler(c *gin.Context) {
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
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	// Basic validation
	if body.Title == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "title required"})
		return
	}
	typeVal := strings.ToLower(body.Type)
	if typeVal == "" {
		typeVal = models.WatchlistTypeMovie
	}
	if typeVal != models.WatchlistTypeMovie && typeVal != models.WatchlistTypeShow {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid type"})
		return
	}
	statusVal := body.Status
	if statusVal == "" {
		statusVal = models.WatchlistStatusPlanned
	}
	if statusVal != models.WatchlistStatusPlanned && statusVal != models.WatchlistStatusWatching && statusVal != models.WatchlistStatusFinished {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	if body.Year != 0 {
		if body.Year < 1870 || body.Year > time.Now().Year()+1 { // basic sanity
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid year"})
			return
		}
	}

	var ext models.ExternalIDs
	if body.TMDB != nil {
		ext.TMDB = *body.TMDB
	}
	if body.IMDB != "" {
		ext.IMDB = body.IMDB
	}

	item := &models.WatchlistItem{
		ID:          primitive.NilObjectID,
		UserID:      user.ID,
		Title:       body.Title,
		Type:        typeVal,
		Year:        body.Year,
		ExternalIDs: ext,
		Tags:        body.Tags,
		Status:      statusVal,
		AddedAt:     time.Time{}, // set in DB layer
		UpdatedAt:   time.Time{},
	}

	if err := s.db.CreateWatchlistItem(c.Request.Context(), item); err != nil {
		if errors.Is(err, database.ErrDuplicateWatchlistItem) {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "duplicate item"})
			return
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to create"})
		return
	}

	c.JSON(http.StatusCreated, item)
}

func (s *Server) listWatchlistItemsHandler(c *gin.Context) {
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

	// Parse query params
	q := c.Request.URL.Query()
	limit := 20
	if v := q.Get("limit"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n > 0 {
			if n > 100 {
				n = 100
			}
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if n, convErr := strconv.Atoi(v); convErr == nil && n >= 0 {
			offset = n
		}
	}
	status := strings.ToLower(q.Get("status"))
	if status != "" && status != models.WatchlistStatusPlanned && status != models.WatchlistStatusWatching && status != models.WatchlistStatusFinished {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid status filter"})
		return
	}
	itemType := strings.ToLower(q.Get("type"))
	if itemType != "" && itemType != models.WatchlistTypeMovie && itemType != models.WatchlistTypeShow {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid type filter"})
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
