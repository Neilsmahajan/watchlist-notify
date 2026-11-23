package server

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/cache"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/imdbsync"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/imdb"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/tonight"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

type Server struct {
	port  int
	db    database.Service
	cache cache.Service // TMDb search results
	tmdb  *tmdb.Client  // nil without TMDB_API_KEY: search and import are off
	// refresher fetches and stores availability for titles that have none yet.
	refresher *availability.Refresher
	// lister builds the watchlist view served by GET /api/watchlist.
	lister              *watchlist.Lister
	defaultRegion       string
	notificationsSender *notifications.Sender
	// lookupBudget overrides upstreamLookupBudget; tests shorten it.
	lookupBudget time.Duration

	// imdbLists reads a public IMDb list when a user links one.
	imdbLists imdbListFetcher
	// reconciler adds the titles of an IMDb list or CSV export to a
	// watchlist. Without a TMDb key it cannot resolve IMDb IDs, and linking
	// and importing are off.
	reconciler *imdbsync.Reconciler
	// resolveBudget overrides how long a link or an import may spend asking
	// TMDb; tests shorten it.
	resolveBudget time.Duration
	// reconcileDeadline overrides how long a link or an import may take up
	// to the end of its reconcile, database time included; tests shorten it.
	reconcileDeadline time.Duration

	// recommender picks what to watch tonight, with Claude. It is nil
	// without ANTHROPIC_API_KEY: the endpoint then answers 503 and GET
	// /api/me reports the feature as off.
	recommender *tonight.Recommender
}

const (
	defaultPort   = 8080
	fromEmail     = "alerts@watchlistnotify.com"
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

	db := database.New()
	// A nil *tmdb.Client must stay a nil interface, or the refresher would
	// think it has a client.
	var fetcher availability.Fetcher
	if tmdbClient != nil {
		fetcher = tmdbClient
	}
	defaultRegion := database.DefaultRegion()
	refresher := availability.NewRefresher(fetcher, db, nil)
	var finder imdbsync.Finder
	if tmdbClient != nil {
		finder = tmdbClient
	}

	lister := watchlist.NewLister(db, refresher, defaultRegion)

	NewServer := &Server{
		port:                port,
		db:                  db,
		cache:               cache.New(),
		tmdb:                tmdbClient,
		refresher:           refresher,
		lister:              lister,
		defaultRegion:       defaultRegion,
		notificationsSender: notificationsSender,
		imdbLists:           imdb.New(),
		reconciler:          imdbsync.New(db, finder),
		recommender:         newRecommender(lister, db, os.Getenv),
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
