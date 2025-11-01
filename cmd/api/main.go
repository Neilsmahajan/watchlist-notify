package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/server"
)

// @title Watchlist Notify API
// @version 0.9.0
// @description API for managing movie and TV show watchlists with streaming availability notifications across multiple platforms.
// @description
// @description Features:
// @description - User profile and preferences management
// @description - Watchlist CRUD operations for movies and TV shows
// @description - Streaming service subscription management
// @description - Real-time availability checking via TMDb
// @description - Digest email notifications with customizable schedules
// @description - Content search and discovery
//
// @contact.name Watchlist Notify Support
// @contact.url https://watchlistnotify.com/contact
// @contact.email contact@watchlistnotify.com
//
// @license.name MIT
// @license.url https://github.com/neilsmahajan/watchlist-notify/blob/main/LICENCE
//
// @host api.watchlistnotify.com
// @BasePath /
// @schemes https http
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token from Auth0 authentication.

func gracefulShutdown(apiServer *http.Server, done chan bool) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	<-ctx.Done()

	log.Println("shutting down gracefully, press Ctrl+C again to force")
	stop() // Allow Ctrl+C to force shutdown

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := apiServer.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown with error: %v", err)
	}

	log.Println("Server exiting")

	done <- true
}

func main() {
	newServer := server.NewServer()

	done := make(chan bool, 1)

	go gracefulShutdown(newServer, done)

	err := newServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(fmt.Sprintf("http newServer error: %s", err))
	}

	<-done
	log.Println("Graceful shutdown complete.")
}
