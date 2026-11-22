package worker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/digest"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
)

func TestParseTasks(t *testing.T) {
	tests := []struct {
		spec    string
		want    []string
		wantErr bool
	}{
		{"", []string{TaskRefresh, TaskDigest}, false},
		{" , ", []string{TaskRefresh, TaskDigest}, false},
		{"refresh", []string{TaskRefresh}, false},
		{"digest", []string{TaskDigest}, false},
		{"refresh,digest", []string{TaskRefresh, TaskDigest}, false},
		{"digest, refresh", []string{TaskRefresh, TaskDigest}, false}, // always run order
		{"Digest,DIGEST", []string{TaskDigest}, false},
		{"refresh,imdb,digest", []string{TaskRefresh, TaskIMDb, TaskDigest}, false},
		{"refresh,emails", nil, true},
	}
	for _, tt := range tests {
		got, err := ParseTasks(tt.spec)
		if (err != nil) != tt.wantErr || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseTasks(%q) = (%v, %v), want (%v, error %v)", tt.spec, got, err, tt.want, tt.wantErr)
		}
	}
}

// taskFunc adapts a function to Task.
type taskFunc func(ctx context.Context) error

func (f taskFunc) Run(ctx context.Context) error { return f(ctx) }

func TestRunRunsTasksInOrderAndKeepsGoing(t *testing.T) {
	var ran []string
	factory := func(name string, err error) Factory {
		return func() (Task, error) {
			return taskFunc(func(context.Context) error {
				ran = append(ran, name)
				return err
			}), nil
		}
	}
	boom := errors.New("tmdb unreachable")
	factories := map[string]Factory{
		TaskRefresh: factory(TaskRefresh, boom),
		TaskDigest:  factory(TaskDigest, nil),
	}

	err := Run(context.Background(), nil, []string{TaskRefresh, TaskDigest}, factories)

	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "refresh:") {
		t.Errorf("Run error = %v, want the refresh failure", err)
	}
	if want := []string{TaskRefresh, TaskDigest}; !reflect.DeepEqual(ran, want) {
		t.Errorf("ran %v, want %v: a failed refresh must not hold back the digest", ran, want)
	}

	ran = nil
	factories[TaskRefresh] = factory(TaskRefresh, nil)
	if err := Run(context.Background(), nil, []string{TaskRefresh, TaskDigest}, factories); err != nil {
		t.Errorf("Run with healthy tasks: %v", err)
	}
}

func TestRunRejectsUnavailableTasksBeforeRunningAny(t *testing.T) {
	ran := false
	factories := map[string]Factory{
		TaskRefresh: func() (Task, error) {
			return taskFunc(func(context.Context) error { ran = true; return nil }), nil
		},
	}
	// imdb is a known name without an implementation yet.
	err := Run(context.Background(), nil, []string{TaskRefresh, TaskIMDb}, factories)
	if err == nil || !strings.Contains(err.Error(), "imdb") {
		t.Errorf("Run error = %v, want it to name the missing task", err)
	}
	if ran {
		t.Error("a task ran although the selection was invalid")
	}
}

func TestRunReportsFactoryErrors(t *testing.T) {
	missing := errors.New("TMDB_API_KEY is not set")
	err := Run(context.Background(), nil, []string{TaskRefresh}, map[string]Factory{
		TaskRefresh: func() (Task, error) { return nil, missing },
	})
	if !errors.Is(err, missing) {
		t.Errorf("Run error = %v, want the factory's error", err)
	}
}

func TestRunDoesNotStartTasksAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	err := Run(ctx, nil, []string{TaskDigest}, map[string]Factory{
		TaskDigest: func() (Task, error) {
			return taskFunc(func(context.Context) error { ran = true; return nil }), nil
		},
	})
	if !errors.Is(err, context.Canceled) || ran {
		t.Errorf("Run = %v, ran = %v; want a cancellation error and no task run", err, ran)
	}
}

// fakeRefresher records what it was asked to refresh and returns a summary.
type fakeRefresher struct {
	refs    []models.TitleRef
	regions []string
	summary availability.Summary
	err     error
}

func (f *fakeRefresher) RefreshAll(_ context.Context, refs []models.TitleRef, regions []string) (availability.Summary, error) {
	f.refs, f.regions = refs, regions
	f.summary.Titles = len(refs)
	return f.summary, f.err
}

func refreshStore() *storetest.Store {
	store := storetest.New()
	us := store.AddUser(&models.User{Email: "us@example.com", Region: "US"})
	gb := store.AddUser(&models.User{Email: "gb@example.com", Region: "gb"})
	store.AddItem(&models.WatchlistItem{UserID: us.ID, Type: models.WatchlistTypeMovie, TMDbID: 603})
	store.AddItem(&models.WatchlistItem{UserID: gb.ID, Type: models.WatchlistTypeMovie, TMDbID: 603}) // same title
	store.AddItem(&models.WatchlistItem{UserID: gb.ID, Type: models.WatchlistTypeShow, TMDbID: 1399})
	store.AddItem(&models.WatchlistItem{UserID: gb.ID, Type: models.WatchlistTypeMovie, Title: "unlinked"})
	return store
}

func TestRefreshTaskCoversDistinctTitlesAndUserRegions(t *testing.T) {
	refresher := &fakeRefresher{summary: availability.Summary{Refreshed: 2}}
	task := &RefreshTask{Store: refreshStore(), Refresher: refresher, DefaultRegion: "CA"}

	if err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantRefs := []models.TitleRef{{MediaType: "movie", TMDbID: 603}, {MediaType: "tv", TMDbID: 1399}}
	if !reflect.DeepEqual(refresher.refs, wantRefs) {
		t.Errorf("refreshed %v, want %v", refresher.refs, wantRefs)
	}
	if want := []string{"CA", "GB", "US"}; !reflect.DeepEqual(refresher.regions, want) {
		t.Errorf("regions = %v, want %v (user regions plus the default)", refresher.regions, want)
	}
}

func TestRefreshTaskFailsOnlyWhenNothingWorked(t *testing.T) {
	tests := []struct {
		name    string
		summary availability.Summary
		wantErr bool
	}{
		{"all refreshed", availability.Summary{Refreshed: 2}, false},
		{"some failed", availability.Summary{Refreshed: 1, Failed: 1}, false},
		{"only tombstones", availability.Summary{NotFound: 2}, false},
		{"everything failed", availability.Summary{Failed: 2}, true},
		{"nothing was due", availability.Summary{Fresh: 2}, false},
		{"the one due title failed", availability.Summary{Fresh: 1, Failed: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &RefreshTask{Store: refreshStore(), Refresher: &fakeRefresher{summary: tt.summary}, DefaultRegion: "US"}
			if err := task.Run(context.Background()); (err != nil) != tt.wantErr {
				t.Errorf("Run() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}

	t.Run("empty watchlists are not a failure", func(t *testing.T) {
		task := &RefreshTask{Store: storetest.New(), Refresher: &fakeRefresher{}, DefaultRegion: "US"}
		if err := task.Run(context.Background()); err != nil {
			t.Errorf("Run() error = %v", err)
		}
	})
	t.Run("store failure", func(t *testing.T) {
		store := refreshStore()
		store.Err = errors.New("database down")
		task := &RefreshTask{Store: store, Refresher: &fakeRefresher{}, DefaultRegion: "US"}
		if err := task.Run(context.Background()); err == nil {
			t.Error("Run() succeeded without a database")
		}
	})
}

type fakeDigestRunner struct {
	summary digest.Summary
	err     error
}

func (f fakeDigestRunner) Run(context.Context) (digest.Summary, error) { return f.summary, f.err }

func TestDigestTaskPassesTheRunnersVerdictOn(t *testing.T) {
	ok := &DigestTask{Runner: fakeDigestRunner{summary: digest.Summary{Mode: digest.ModeDryRun, Due: 2, DryRun: 1, NotAllowlisted: 1, Duration: time.Second}}}
	if err := ok.Run(context.Background()); err != nil {
		t.Errorf("Run() error = %v", err)
	}
	failed := &DigestTask{Runner: fakeDigestRunner{err: errors.New("all 2 due users failed")}}
	if err := failed.Run(context.Background()); err == nil {
		t.Error("Run() swallowed the runner's error")
	}
}
