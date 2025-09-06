package server

import (
	"net/http"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
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
	r.GET("/auth/google/login", s.authLoginHandler)
	r.GET("/auth/google/callback", s.authCallbackHandler)
	r.POST("/auth/logout", s.logoutHandler)

	protected := r.Group("/api")
	protected.Use(middleware.AuthRequired())
	protected.GET("/me", s.meHandler)
	protected.PATCH("/me/preferences", s.updateUserPreferencesHandler)
	protected.GET("/me/services", s.listUserServicesHandler)
	protected.PATCH("/me/services", s.updateUserServicesHandler)
	protected.POST("/watchlist", s.createWatchlistItemHandler)
	protected.GET("/watchlist", s.listWatchlistItemsHandler)
	protected.PATCH("/watchlist/:id", s.updateWatchlistItemHandler)
	protected.DELETE("/watchlist/:id", s.deleteWatchlistItemHandler)
	protected.GET("/search", s.searchHandler)
	return r
}
