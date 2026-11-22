// Command worker runs the scheduled background tasks. It is deployed as a
// Cloud Run job and triggered once a day.
//
//	worker                      # refresh, then digest (dry run)
//	WORKER_TASKS=refresh worker # only refresh stored availability
//	worker -tasks digest        # only the digest
//
// See README.md next to this file for the environment it reads.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/digest"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/worker"
)

const defaultSiteURL = "https://watchlistnotify.com"

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	tasksFlag := flag.String("tasks", "", "comma-separated tasks to run (overrides WORKER_TASKS); default: refresh,digest")
	flag.Parse()

	spec := os.Getenv("WORKER_TASKS")
	if strings.TrimSpace(*tasksFlag) != "" {
		spec = *tasksFlag
	}
	tasks, err := worker.ParseTasks(spec)
	if err != nil {
		logger.Error("worker: invalid task selection", "error", err.Error())
		return 2
	}

	// Cloud Run sends SIGTERM when a job execution times out or is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("worker starting", "tasks", tasks)
	db := database.New()

	factories := map[string]worker.Factory{
		worker.TaskRefresh: func() (worker.Task, error) { return newRefreshTask(db, logger) },
		worker.TaskDigest:  func() (worker.Task, error) { return newDigestTask(db, logger) },
	}
	if err := worker.Run(ctx, logger, tasks, factories); err != nil {
		logger.Error("worker finished with errors", "error", err.Error())
		return 1
	}
	logger.Info("worker finished")
	return 0
}

func newRefreshTask(db database.Service, logger *slog.Logger) (worker.Task, error) {
	apiKey := strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("TMDB_API_KEY is not set")
	}
	client, err := tmdb.New(apiKey)
	if err != nil {
		return nil, err
	}
	return &worker.RefreshTask{
		Store:         db,
		Refresher:     availability.NewRefresher(client, db, logger),
		DefaultRegion: database.DefaultRegion(),
		Logger:        logger,
	}, nil
}

func newDigestTask(db database.Service, logger *slog.Logger) (worker.Task, error) {
	mode, err := digest.ParseMode(os.Getenv("DIGEST_MODE"))
	if err != nil {
		return nil, err
	}
	allowlist := digest.ParseAllowlist(os.Getenv("DIGEST_ALLOWLIST"))

	// Dry runs never build a mailer, so they need no Postmark token and
	// cannot send even by accident.
	var mailer digest.Mailer
	if mode == digest.ModeSend {
		sender, err := notifications.New(notifications.DefaultFromEmail, notifications.DefaultMessageStream)
		if err != nil {
			return nil, err
		}
		mailer = sender
		if len(allowlist) == 0 {
			logger.Warn("digest: DIGEST_MODE=send but DIGEST_ALLOWLIST is empty; no one will be emailed")
		}
	}

	siteURL := strings.TrimSpace(os.Getenv("FRONTEND_URL"))
	if siteURL == "" {
		siteURL = defaultSiteURL
	}

	runner, err := digest.NewRunner(db, mailer, digest.Config{
		Mode:          mode,
		Allowlist:     allowlist,
		SiteURL:       siteURL,
		DefaultRegion: database.DefaultRegion(),
	}, logger)
	if err != nil {
		return nil, err
	}
	logger.Info("digest configured", "mode", string(mode), "allowlisted_accounts", len(allowlist))
	return &worker.DigestTask{Runner: runner, Logger: logger}, nil
}
