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
	minAge, err := refreshMinAge(os.Getenv("REFRESH_MIN_AGE"))
	if err != nil {
		return nil, err
	}
	logger.Info("refresh configured", "min_age", minAge.String())
	return &worker.RefreshTask{
		Store:         db,
		Refresher:     availability.NewRefresher(client, db, logger, availability.WithMinAge(minAge)),
		DefaultRegion: database.DefaultRegion(),
		Logger:        logger,
	}, nil
}

// defaultRefreshMinAge keeps a title from being fetched twice in one day when
// the job is triggered more often than daily, while still letting a daily
// trigger that drifts by a few hours refresh everything.
const defaultRefreshMinAge = 20 * time.Hour

// refreshMinAge parses REFRESH_MIN_AGE, a Go duration such as "20h". "0"
// refreshes every title on every run.
func refreshMinAge(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultRefreshMinAge, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("REFRESH_MIN_AGE %q is not a duration such as 20h or 0", value)
	}
	return d, nil
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
