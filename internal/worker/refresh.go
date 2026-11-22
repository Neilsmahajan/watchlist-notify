package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// RefreshStore lists what the refresh has to cover.
type RefreshStore interface {
	DistinctWatchlistTitles(ctx context.Context) ([]models.TitleRef, error)
	DistinctUserRegions(ctx context.Context) ([]string, error)
}

// TitleRefresher refreshes a set of titles; *availability.Refresher does.
type TitleRefresher interface {
	RefreshAll(ctx context.Context, refs []models.TitleRef, regions []string) (availability.Summary, error)
}

// RefreshTask refreshes stored availability for every title on any watchlist,
// in every region a user is in.
type RefreshTask struct {
	Store         RefreshStore
	Refresher     TitleRefresher
	DefaultRegion string
	Logger        *slog.Logger
}

// Run refreshes all titles and logs a summary. Individual titles may fail;
// the task fails only when it could not start or nothing at all worked.
func (t *RefreshTask) Run(ctx context.Context) error {
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}

	refs, err := t.Store.DistinctWatchlistTitles(ctx)
	if err != nil {
		return fmt.Errorf("list watchlist titles: %w", err)
	}
	userRegions, err := t.Store.DistinctUserRegions(ctx)
	if err != nil {
		return fmt.Errorf("list user regions: %w", err)
	}
	regions := availability.TrackedRegions(userRegions, t.DefaultRegion)

	sum, err := t.Refresher.RefreshAll(ctx, refs, regions)
	if err != nil {
		return err
	}

	logger.Info("refresh summary",
		"titles", sum.Titles,
		"regions", regions,
		"refreshed", sum.Refreshed,
		"baselines", sum.Baselines,
		"changed", sum.Changed,
		"events", sum.Events,
		"not_found", sum.NotFound,
		"superseded", sum.Superseded,
		"failures", sum.Failed,
		"skipped", sum.Skipped,
		"duration_ms", sum.Duration.Milliseconds(),
	)
	// These two keep the provider mapping honest: a service that only matches
	// by name should get its ID added to the table, and an unmapped provider
	// that is really one of our services is a gap in it.
	if len(sum.NameMatched) > 0 {
		logger.Info("refresh: providers matched by name only", "providers", sum.NameMatched)
	}
	if len(sum.Unmapped) > 0 {
		logger.Info("refresh: streaming providers without a service code", "providers", sum.Unmapped)
	}

	if sum.Titles > 0 && sum.Refreshed+sum.NotFound == 0 {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("refresh stopped before any title was refreshed: %w", err)
		}
		return errors.New("refresh: no title could be refreshed")
	}
	return nil
}
