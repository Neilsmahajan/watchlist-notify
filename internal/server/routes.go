package server

import (
	"errors"
	"net/http"
	"os"
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
	emailVal, exists := c.Get("user_email")
	if !exists {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	email, _ := emailVal.(string)
	user, err := s.db.GetUserByEmail(c.Request.Context(), email)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch user"})
		return
	}
	if user == nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, user)
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

	items, err := s.db.ListWatchlistItems(c.Request.Context(), user.ID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch items"})
		return
	}
	c.JSON(http.StatusOK, items)
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
	c.JSON(http.StatusOK, gin.H{"message": "watchlist item deleted"})
}
