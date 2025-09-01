package server

import (
	"net/http"
	"os"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/auth"
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

	r.GET("/", s.HelloWorldHandler)
	r.GET("/health", s.healthHandler)
	r.GET("/auth/google/login", auth.LoginHandler)
	r.GET("/auth/google/callback", func(c *gin.Context) { auth.CallbackHandler(c, s.db) })

	protected := r.Group("/api")
	protected.Use(middleware.AuthRequired())
	protected.GET("/me", func(c *gin.Context) {
		email, _ := c.Get("user_email")
		c.JSON(http.StatusOK, gin.H{"email": email})
	})
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
