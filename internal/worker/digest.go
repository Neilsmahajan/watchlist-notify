package worker

import (
	"context"
	"log/slog"

	"github.com/neilsmahajan/watchlist-notify/internal/digest"
)

// DigestRunner sends the digests that are due; *digest.Runner does.
type DigestRunner interface {
	Run(ctx context.Context) (digest.Summary, error)
}

// DigestTask emails users about titles that newly became available.
type DigestTask struct {
	Runner DigestRunner
	Logger *slog.Logger
}

// Run processes the due users and logs counts. The log carries no email
// addresses and no email content.
func (t *DigestTask) Run(ctx context.Context) error {
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	sum, err := t.Runner.Run(ctx)
	logger.Info("digest summary",
		"mode", string(sum.Mode),
		"due", sum.Due,
		"recipients", sum.Recipients(),
		"titles", sum.Titles,
		"sent", sum.Sent,
		"dry_run", sum.DryRun,
		"not_allowlisted", sum.NotAllowlisted,
		"nothing_new", sum.NothingNew,
		"claim_lost", sum.ClaimLost,
		"send_failed", sum.SendFailed,
		"send_uncertain", sum.SendUncertain,
		"errors", sum.Errors,
		"duration_ms", sum.Duration.Milliseconds(),
	)
	if sum.Mode == digest.ModeDryRun && sum.DryRun > 0 {
		logger.Info("digest: dry run, nothing was sent or written", "would_email", sum.DryRun, "titles", sum.Titles)
	}
	if sum.NotAllowlisted > 0 {
		logger.Warn("digest: users due but not allowlisted", "users", sum.NotAllowlisted)
	}
	return err
}
