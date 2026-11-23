package server

import (
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	_ "github.com/joho/godotenv/autoload"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/neilsmahajan/watchlist-notify/docs" // Import generated docs
	"github.com/neilsmahajan/watchlist-notify/internal/middleware"
)

// RegisterRoutes builds the API's handler from the environment.
func (s *Server) RegisterRoutes() http.Handler {
	mcpCfg, err := loadMCPConfig(os.Getenv("MCP_RESOURCE_URL"), os.Getenv("AUTH0_DOMAIN"))
	if err != nil {
		log.Fatalf("mcp: %v", err)
	}
	handler, err := s.routes(routeConfig{
		origins:  allowedOrigins(os.Getenv("FRONTEND_URL"), os.Getenv("APP_ENV")),
		restAuth: middleware.EnsureValidToken(),
		mcp:      mcpCfg,
	})
	if err != nil {
		log.Fatalf("routes: %v", err)
	}
	return handler
}

// routeConfig is what the routes depend on outside the Server.
type routeConfig struct {
	// origins are the browser origins that may call the REST API.
	origins []string
	// restAuth guards the REST API under /api.
	restAuth gin.HandlerFunc
	// mcp configures the MCP endpoint. Nil leaves it, and its metadata
	// routes, unregistered.
	mcp *mcpConfig
}

// routes builds the API's handler.
func (s *Server) routes(cfg routeConfig) (http.Handler, error) {
	r := gin.Default()

	// The MCP endpoint and its metadata take neither of the two global
	// middlewares below; see exceptMCP.
	global := func(mw gin.HandlerFunc) gin.HandlerFunc { return mw }
	if cfg.mcp != nil {
		global = exceptMCP
	}

	// Compress responses for clients that accept it: a page of the watchlist
	// is mostly repeated JSON keys and prose. Swagger UI serves its own
	// pre-built assets and is left alone.
	r.Use(global(gzip.Gzip(gzip.DefaultCompression, gzip.WithExcludedPaths([]string{"/swagger"}))))

	// With no allowed origin there is nothing for the middleware to do, and
	// browsers already refuse cross-origin reads of responses without CORS
	// headers.
	if len(cfg.origins) > 0 {
		r.Use(global(newCORS(cfg.origins)))
	} else {
		log.Println("cors: FRONTEND_URL is not set to a valid origin; cross-origin requests are not allowed")
	}

	if cfg.mcp != nil {
		if err := s.registerMCP(r, cfg.mcp, cfg.origins); err != nil {
			return nil, err
		}
		log.Printf("mcp: serving %s for tokens issued by %s", cfg.mcp.resourceURL, cfg.mcp.issuer)
	}

	r.GET("/health", s.healthHandler)
	// The public demo: read-only, no token, one seeded account.
	r.GET("/api/demo/watchlist", s.demoWatchlistHandler)
	r.GET("/api/demo/services", s.demoServicesHandler)

	// Swagger documentation endpoint
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	protected := r.Group("/api")

	// Use Auth0 JWT validation middleware for protected routes (native Gin middleware)
	protected.Use(cfg.restAuth)

	protected.GET("/me", s.meHandler)
	protected.PATCH("/me/preferences", s.updateUserPreferencesHandler)
	protected.GET("/me/services", s.listUserServicesHandler)
	protected.PATCH("/me/services", s.updateUserServicesHandler)
	protected.POST("/me/notifications/test", s.testNotificationHandler)
	protected.PUT("/me/imdb", s.linkIMDbHandler)
	protected.DELETE("/me/imdb", s.unlinkIMDbHandler)

	protected.POST("/watchlist", s.createWatchlistItemHandler)
	protected.GET("/watchlist", s.listWatchlistItemsHandler)
	protected.PATCH("/watchlist/:id", s.updateWatchlistItemHandler)
	protected.DELETE("/watchlist/:id", s.deleteWatchlistItemHandler)
	protected.POST("/watchlist/import", s.importWatchlistHandler)

	protected.GET("/search", s.searchHandler)

	protected.GET("/availability/:id", s.availabilityHandler)
	protected.POST("/availability/batch", s.batchAvailabilityHandler)

	// Registered with or without an Anthropic key: without one it answers 503.
	protected.POST("/recommendations/tonight", s.tonightHandler)

	if cfg.mcp != nil {
		return withoutMCPQuery(r), nil
	}
	return r, nil
}

// newCORS allows browser calls, with credentials, from exactly origins.
func newCORS(origins []string) gin.HandlerFunc {
	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Accept", "Authorization", "Content-Type"},
		ExposeHeaders:    []string{"Server-Timing"},
		AllowCredentials: true,
	})
}

// localOrigins are allowed outside production, for the frontend dev server.
var localOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}

// allowedOrigins returns the origins that may call the API from a browser:
// the frontend's origin, plus the local dev server unless appEnv is
// "production". A wildcard is never allowed, because the API accepts
// credentials.
func allowedOrigins(frontendURL, appEnv string) []string {
	var origins []string
	if origin, ok := originOf(frontendURL); ok {
		origins = append(origins, origin)
	}
	if !strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		for _, origin := range localOrigins {
			if len(origins) == 0 || origins[0] != origin {
				origins = append(origins, origin)
			}
		}
	}
	return origins
}

// originOf reduces a URL to its origin (scheme://host[:port]). It rejects
// anything that is not an absolute http(s) URL.
func originOf(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}
