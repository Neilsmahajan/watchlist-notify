package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

type Server struct {
	port                int
	db                  database.Service
	cache               cache.Service
	tmdb                *tmdb.Client
	notificationsSender *notifications.Sender
}

const (
	defaultPort   = 8080
	fromEmail     = "contact@watchlistnotify.com"
	messageStream = "outbound"
)

func NewServer() *http.Server {
	port, err := strconv.Atoi(os.Getenv("PORT"))
	if err != nil || port <= 0 {
		port = defaultPort
	}

	tmdbKey := os.Getenv("TMDB_API_KEY")
	var tmdbClient *tmdb.Client
	if tmdbKey != "" {
		if c, err := tmdb.New(tmdbKey); err == nil {
			tmdbClient = c
		}
	}

	// Initialize notifications sender - it will check POSTMARK_SERVER_TOKEN internally
	notificationsSender, err := notifications.New(fromEmail, messageStream)
	if err != nil {
		// Log warning but don't fail server startup
		fmt.Printf("Warning: Failed to initialize notifications sender: %v\n", err)
		notificationsSender = nil
	}

	NewServer := &Server{
		port:                port,
		db:                  database.New(),
		cache:               cache.New(),
		tmdb:                tmdbClient,
		notificationsSender: notificationsSender,
	}

	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}
