package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"

	"github.com/neilsmahajan/watchlist-notify/internal/auth"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/util"
)

type Server struct {
	port  int
	db    database.Service
	tmdb  *tmdb.Client
	cache *util.Cache
}

func NewServer() *http.Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))

	auth.Init()
	tmdbKey := os.Getenv("TMDB_API_KEY")
	var tmdbClient *tmdb.Client
	if tmdbKey != "" {
		if c, err := tmdb.New(tmdbKey); err == nil {
			tmdbClient = c
		}
	}
	NewServer := &Server{
		port:  port,
		db:    database.New(),
		tmdb:  tmdbClient,
		cache: util.NewCache(),
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
