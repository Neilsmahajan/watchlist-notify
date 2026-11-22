// Package worker runs the scheduled background tasks: refreshing stored
// streaming availability and sending digest emails. One process runs the
// selected tasks in a fixed order, so a digest always sees the availability
// the same run just refreshed.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Task names, as used in WORKER_TASKS.
const (
	TaskRefresh = "refresh"
	TaskIMDb    = "imdb"
	TaskDigest  = "digest"
)

// order is the sequence tasks run in, whatever order they were asked for.
// Each task feeds the next: refresh records what changed, imdb (not built
// yet) will pull list changes in between, and digest reports on the result.
var order = []string{TaskRefresh, TaskIMDb, TaskDigest}

// defaultTasks run when no selection is given.
var defaultTasks = []string{TaskRefresh, TaskDigest}

// Task is one unit of scheduled work.
type Task interface {
	Run(ctx context.Context) error
}

// Factory builds a task. Tasks are built only when selected, so running just
// the refresh does not need, say, email credentials.
type Factory func() (Task, error)

// ParseTasks reads a task selection such as "refresh,digest". Empty means the
// default set. The result is in run order with duplicates removed; an unknown
// name is an error.
func ParseTasks(spec string) ([]string, error) {
	selected := make(map[string]bool)
	for _, part := range strings.Split(spec, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if !known(name) {
			return nil, fmt.Errorf("unknown task %q (known: %s)", name, strings.Join(order, ", "))
		}
		selected[name] = true
	}
	if len(selected) == 0 {
		return append([]string(nil), defaultTasks...), nil
	}
	var tasks []string
	for _, name := range order {
		if selected[name] {
			tasks = append(tasks, name)
		}
	}
	return tasks, nil
}

func known(name string) bool {
	for _, n := range order {
		if n == name {
			return true
		}
	}
	return false
}

// Run builds and runs the named tasks in the given order. A task that fails
// does not stop the ones after it: a refresh that could not reach TMDb is no
// reason to hold back a digest about yesterday's changes. The error joins
// every task's failure, and is nil only if all of them succeeded.
//
// Every name must have a factory. That is checked before anything runs, so a
// typo or a task this build does not have fails fast instead of half-running.
func Run(ctx context.Context, logger *slog.Logger, names []string, factories map[string]Factory) error {
	if logger == nil {
		logger = slog.Default()
	}
	for _, name := range names {
		if factories[name] == nil {
			return fmt.Errorf("task %q is not available in this build", name)
		}
	}

	var errs []error
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("%s: not started: %w", name, err))
			continue
		}
		start := time.Now()
		logger.Info("task starting", "task", name)

		task, err := factories[name]()
		if err == nil {
			err = task.Run(ctx)
		}
		if err != nil {
			logger.Error("task failed", "task", name, "error", err.Error(), "duration_ms", time.Since(start).Milliseconds())
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		logger.Info("task finished", "task", name, "duration_ms", time.Since(start).Milliseconds())
	}
	return errors.Join(errs...)
}
