// Command seed-demo creates the account behind the public demo
// (GET /api/demo/watchlist) and fills its watchlist.
//
//	seed-demo -dry-run   # report what would change; write nothing
//	seed-demo            # create what is missing
//
// It is idempotent: it adds the demo user, its services and the titles in
// titles.csv when they are missing, stores availability baselines for those
// titles through the same refresher the API and the worker use, and changes
// nothing that is already there. It never reads or writes another user.
//
// It reads the API's environment: MONGODB_URI (or DB_HOST, DB_PORT,
// DB_USERNAME, DB_ROOT_PASSWORD), DB_DATABASE and TMDB_API_KEY. A .env file
// in the working directory is loaded. The output is counts only.
//
// The demo account has digests off and no consents, so the worker's digest
// task never selects it, and nobody can sign in as it: the API refuses its
// address (see internal/server/handlers_demo.go).
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
)

// runTimeout bounds the whole run; fetching 76 titles takes a few seconds.
const runTimeout = 5 * time.Minute

func main() {
	os.Exit(run())
}

func run() int {
	dryRun := flag.Bool("dry-run", false, "report what would change without writing anything")
	flag.Parse()

	titles, err := parseTitles(titlesCSV)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed-demo:", err)
		return 2
	}

	// Fetching availability is the last step. Refuse to start a run that
	// could not finish it, rather than leave a demo without posters.
	var client *tmdb.Client
	if !*dryRun {
		apiKey := strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
		if apiKey == "" {
			fmt.Fprintln(os.Stderr, "seed-demo: TMDB_API_KEY is not set (use -dry-run to preview without it)")
			return 2
		}
		if client, err = tmdb.New(apiKey); err != nil {
			fmt.Fprintln(os.Stderr, "seed-demo:", err)
			return 2
		}
	}

	db := database.New()
	var fetch ensurer // a dry run never fetches
	if client != nil {
		// The refresher reports titles it could not fetch as warnings, by
		// title key. Anything quieter is left out so the output stays the
		// report.
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		fetch = availability.NewRefresher(client, db, logger)
	}
	return seedAndReport(db, fetch, titles, *dryRun)
}

func seedAndReport(db store, fetch ensurer, titles []seedTitle, dryRun bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	rep, err := seed(ctx, db, fetch, titles, time.Now(), dryRun)
	fmt.Print(rep)
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed-demo: stopped early:", err)
		return 1
	}
	if rep.TitlesUnresolved > 0 {
		return 1
	}
	return 0
}
