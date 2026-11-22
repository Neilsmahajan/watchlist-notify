package imdbsync

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var planNow = time.Date(2026, 3, 10, 6, 0, 0, 0, time.UTC)

// planLinked is when the list of a worker-sync case was linked, unless the
// case says otherwise: long before any mark the cases use.
var planLinked = planNow.Add(-30 * 24 * time.Hour)

// noLink stands for "no LinkedAt at all" in a table case.
var noLink = time.Unix(1, 0)

// Test items are named by a single letter; their ObjectIDs are derived from
// it so that expectations can name items too.
func itemID(name string) primitive.ObjectID {
	var id primitive.ObjectID
	copy(id[:], name)
	return id
}

type itemOpt func(*models.WatchlistItem)

func imdbID(id string) itemOpt   { return func(w *models.WatchlistItem) { w.IMDbID = id } }
func source(s string) itemOpt    { return func(w *models.WatchlistItem) { w.Source = s } }
func status(s string) itemOpt    { return func(w *models.WatchlistItem) { w.Status = s } }
func tagged(t ...string) itemOpt { return func(w *models.WatchlistItem) { w.Tags = t } }
func show() itemOpt              { return func(w *models.WatchlistItem) { w.Type = models.WatchlistTypeShow } }
func tmdbID(id int) itemOpt      { return func(w *models.WatchlistItem) { w.TMDbID = id } }

// missingFor marks the item missing since d before planNow.
func missingFor(d time.Duration) itemOpt {
	return func(w *models.WatchlistItem) {
		since := planNow.Add(-d)
		w.IMDbMissingSince = &since
	}
}

// item is a planned movie with no source, IMDb ID or TMDb ID until the
// options say otherwise.
func item(name string, opts ...itemOpt) *models.WatchlistItem {
	w := &models.WatchlistItem{
		ID:     itemID(name),
		Title:  name,
		Type:   models.WatchlistTypeMovie,
		Status: models.WatchlistStatusPlanned,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// synced is an item the sync added and the user has not touched.
func synced(name, imdb string, tmdb int, opts ...itemOpt) *models.WatchlistItem {
	return item(name, append([]itemOpt{imdbID(imdb), tmdbID(tmdb), source(models.SourceIMDb)}, opts...)...)
}

func movie(id int) Target {
	return Target{Ref: models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: id}}
}
func tv(id int) Target {
	return Target{Ref: models.TitleRef{MediaType: models.MediaTypeTV, TMDbID: id}}
}

// want describes a plan by item names and IMDb IDs.
type want struct {
	add     []string          // IMDb IDs, in order
	fill    map[string]string // item name -> IMDb ID
	clear   []string          // item names
	mark    []string
	remove  []string
	prune   PruneDecision
	counts  [4]int // total, existing, unmatched, pending
	repeats int
}

func names(ids []primitive.ObjectID) []string {
	out := []string{}
	for _, id := range ids {
		out = append(out, string(id[:1]))
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}

func checkPlan(t *testing.T, plan Plan, w want) {
	t.Helper()
	gotAdd := []string{}
	for _, add := range plan.Add {
		gotAdd = append(gotAdd, add.IMDbID)
	}
	if !reflect.DeepEqual(gotAdd, append([]string{}, w.add...)) {
		t.Errorf("add = %v, want %v", gotAdd, w.add)
	}
	gotFill := map[string]string{}
	for _, fill := range plan.Fill {
		gotFill[string(fill.ItemID[:1])] = fill.IMDbID
	}
	if wantFill := w.fill; len(gotFill)+len(wantFill) > 0 && !reflect.DeepEqual(gotFill, wantFill) {
		t.Errorf("fill = %v, want %v", gotFill, wantFill)
	}
	if got := names(plan.ClearMissing); !reflect.DeepEqual(got, sorted(w.clear)) {
		t.Errorf("clear missing = %v, want %v", got, w.clear)
	}
	if got := names(plan.MarkMissing); !reflect.DeepEqual(got, sorted(w.mark)) {
		t.Errorf("mark missing = %v, want %v", got, w.mark)
	}
	var removed []primitive.ObjectID
	for _, removal := range plan.Remove {
		removed = append(removed, removal.ItemID)
	}
	if got := names(removed); !reflect.DeepEqual(got, sorted(w.remove)) {
		t.Errorf("remove = %v, want %v", got, w.remove)
	}
	if plan.Prune != w.prune {
		t.Errorf("prune = %q, want %q", plan.Prune, w.prune)
	}
	if got := [4]int{plan.Total, plan.Existing, plan.Unmatched, plan.Pending}; got != w.counts {
		t.Errorf("total, existing, unmatched, pending = %v, want %v", got, w.counts)
	}
	if plan.Repeats != w.repeats {
		t.Errorf("repeats = %d, want %d", plan.Repeats, w.repeats)
	}
	if sum := len(plan.Add) + plan.Existing + plan.Unmatched + plan.Pending; sum != plan.Total || len(plan.Outcomes) != plan.Total {
		t.Errorf("counts do not add up: %d added + %d existing + %d unmatched + %d pending != total %d (%d outcomes)",
			len(plan.Add), plan.Existing, plan.Unmatched, plan.Pending, plan.Total, len(plan.Outcomes))
	}
}

const (
	day = 24 * time.Hour
	// The three IMDb IDs most cases use. They resolve to movies 1, 2 and 3.
	ttA = "tt0000001"
	ttB = "tt0000002"
	ttC = "tt0000003"
)

var abc = map[string]Target{ttA: movie(1), ttB: movie(2), ttC: movie(3)}

func TestBuildPlan(t *testing.T) {
	tests := []struct {
		name string
		in   Input
		want want
	}{
		// --- adding ---
		{
			name: "fresh link adds every title in list order",
			in:   Input{Mode: ModeLink, IDs: []string{ttC, ttA, ttB}, Complete: true, Resolved: abc},
			want: want{add: []string{ttC, ttA, ttB}, prune: PruneSkippedMode, counts: [4]int{3, 0, 0, 0}},
		},
		{
			name: "first worker sync of a new list adds and removes nothing",
			in:   Input{Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true, Resolved: abc},
			want: want{add: []string{ttA, ttB}, prune: PruneConsidered, counts: [4]int{2, 0, 0, 0}},
		},
		{
			name: "running again over an unchanged list does nothing",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB, ttC}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2), synced("c", ttC, 3)},
			},
			want: want{prune: PruneConsidered, counts: [4]int{3, 3, 0, 0}},
		},
		{
			name: "a title new on the list is added next to the ones already synced",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true, Resolved: map[string]Target{ttB: movie(2)},
				Existing: []*models.WatchlistItem{synced("a", ttA, 1)},
			},
			want: want{add: []string{ttB}, prune: PruneConsidered, counts: [4]int{2, 1, 0, 0}},
		},
		{
			name: "the same ID twice in one list counts once",
			in:   Input{Mode: ModeSync, IDs: []string{ttA, ttB, ttA, ttA}, Complete: true, Resolved: abc},
			want: want{add: []string{ttA, ttB}, prune: PruneConsidered, counts: [4]int{2, 0, 0, 0}, repeats: 2},
		},
		{
			name: "two IMDb IDs for one TMDb title add it once",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true,
				Resolved: map[string]Target{ttA: movie(7), ttB: movie(7)},
			},
			want: want{add: []string{ttA}, prune: PruneConsidered, counts: [4]int{2, 1, 0, 0}},
		},
		{
			name: "a movie and a show with the same TMDb ID are two titles",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true,
				Resolved: map[string]Target{ttA: movie(7), ttB: tv(7)},
			},
			want: want{add: []string{ttA, ttB}, prune: PruneConsidered, counts: [4]int{2, 0, 0, 0}},
		},
		{
			name: "a show is added although the user has the movie with the same TMDb ID",
			in: Input{
				Mode: ModeSync, IDs: []string{ttB}, Complete: true, Resolved: map[string]Target{ttB: tv(7)},
				Existing: []*models.WatchlistItem{item("m", tmdbID(7))},
			},
			want: want{add: []string{ttB}, prune: PruneConsidered, counts: [4]int{1, 0, 0, 0}},
		},
		{
			name: "IDs TMDb does not know are counted, not added and not errors",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB, ttC}, Complete: true,
				Resolved: map[string]Target{ttA: movie(1)}, Unmatched: map[string]bool{ttB: true, ttC: true},
			},
			want: want{add: []string{ttA}, prune: PruneConsidered, counts: [4]int{3, 0, 2, 0}},
		},
		{
			name: "IDs that could not be looked up are pending and block removal",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true, Resolved: map[string]Target{ttA: movie(1)},
				Existing: []*models.WatchlistItem{synced("g", "tt0000009", 9, missingFor(2*day))},
			},
			want: want{add: []string{ttA}, prune: PruneSkippedPending, counts: [4]int{2, 0, 0, 1}},
		},

		// --- items the user added themselves ---
		{
			name: "a hand-added item on the list is kept as it is and gets its IMDb ID",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA}, Complete: true, Resolved: abc,
				Existing: []*models.WatchlistItem{item("m", tmdbID(1))},
			},
			want: want{fill: map[string]string{"m": ttA}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a hand-added item that already has another IMDb ID is not rewritten",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA}, Complete: true, Resolved: abc,
				Existing: []*models.WatchlistItem{item("m", tmdbID(1), imdbID("tt0000099"))},
			},
			want: want{prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "two list IDs resolving to one hand-added item fill it once",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA, ttB}, Complete: true,
				Resolved: map[string]Target{ttA: movie(1), ttB: movie(1)},
				Existing: []*models.WatchlistItem{item("m", tmdbID(1))},
			},
			want: want{fill: map[string]string{"m": ttA}, prune: PruneConsidered, counts: [4]int{2, 2, 0, 0}},
		},
		{
			name: "hand-added and CSV items that are not on the list are never marked or removed",
			in: Input{
				Mode: ModeSync, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					item("m", tmdbID(5), imdbID("tt0000005")),
					item("n"),
					item("c", tmdbID(6), imdbID("tt0000006"), source(models.SourceCSV)),
					// Not something the sync writes, but nothing may rest on that.
					item("x", tmdbID(8), imdbID("tt0000008"), missingFor(9*day)),
					item("y", tmdbID(4), imdbID("tt0000004"), source(models.SourceCSV), missingFor(9*day)),
				},
			},
			want: want{clear: []string{"x", "y"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},

		// --- removing: two strikes ---
		{
			name: "strike one: a synced item gone from the list is only marked",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2)},
			},
			want: want{mark: []string{"b"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "strike two: still gone a day later, it is removed",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{remove: []string{"b"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "strike two waits until the mark is twelve hours old",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("b", ttB, 2, missingFor(MinMissingAge-time.Second)),
					synced("c", ttC, 3, missingFor(MinMissingAge)),
				},
			},
			want: want{remove: []string{"c"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a mark from the future (clock skew) is not old enough",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(-time.Hour))},
			},
			want: want{prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "back on the list between the strikes: the mark is cleared",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA, ttB}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{clear: []string{"b"}, prune: PruneConsidered, counts: [4]int{2, 2, 0, 0}},
		},
		{
			name: "back on the list under a new IMDb ID for the same title: the mark is cleared",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA, "tt0000022"}, Complete: true,
				Resolved: map[string]Target{"tt0000022": movie(2)},
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{clear: []string{"b"}, prune: PruneConsidered, counts: [4]int{2, 2, 0, 0}},
		},
		{
			name: "a partial fetch that happens to contain a marked item still clears its mark",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttB}, Complete: false,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1, missingFor(day)), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{clear: []string{"b"}, prune: PruneSkippedIncomplete, counts: [4]int{1, 1, 0, 0}},
		},

		// --- removing: the user's changes win ---
		{
			name: "an item the user started watching is kept forever",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("w", ttB, 2, status(models.WatchlistStatusWatching)),
					synced("f", ttC, 3, status(models.WatchlistStatusFinished)),
				},
			},
			want: want{prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "an item the user changed after strike one is kept and its mark cleared",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("w", ttB, 2, status(models.WatchlistStatusFinished), missingFor(9*day)),
					synced("t", ttC, 3, tagged("date night"), missingFor(9*day)),
				},
			},
			want: want{clear: []string{"t", "w"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "an item the user tagged is kept",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("t", ttB, 2, tagged("favourite"))},
			},
			want: want{prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},

		// --- removing: fetches that prove nothing ---
		{
			name: "a partial fetch adds what it saw and neither marks nor removes",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttA}, Complete: false, Resolved: abc,
				Existing: []*models.WatchlistItem{synced("b", ttB, 2), synced("c", ttC, 3, missingFor(9*day))},
			},
			want: want{add: []string{ttA}, prune: PruneSkippedIncomplete, counts: [4]int{1, 0, 0, 0}},
		},
		{
			name: "a total that does not match is a partial fetch",
			in: Input{
				// The client reports Complete=false when IMDb's total and the
				// entries read disagree; nothing else distinguishes the case.
				Mode: ModeSync, Now: planNow, IDs: []string{ttA, ttB}, Complete: false,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2), synced("c", ttC, 3, missingFor(9*day))},
			},
			want: want{prune: PruneSkippedIncomplete, counts: [4]int{2, 2, 0, 0}},
		},
		{
			name: "an empty list, even a complete one, removes nothing",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: nil, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(9*day))},
			},
			want: want{prune: PruneSkippedEmpty, counts: [4]int{0, 0, 0, 0}},
		},
		{
			name: "a list of nothing but unknown titles is not empty: it can still remove",
			in: Input{
				Mode: ModeSync, Now: planNow, IDs: []string{ttC}, Complete: true, Unmatched: map[string]bool{ttC: true},
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{mark: []string{"a"}, remove: []string{"b"}, prune: PruneConsidered, counts: [4]int{1, 0, 1, 0}},
		},

		// --- strikes belong to one link ---
		{
			name: "a strike from before this link is cleared, not acted on",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: planNow.Add(-day), IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(3*day))},
			},
			// Not removed although it is absent from a complete fetch and the
			// mark is old enough, and not marked again in the same run.
			want: want{clear: []string{"b"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a strike made after the link counts as before",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: planNow.Add(-3 * day), IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day))},
			},
			want: want{remove: []string{"b"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "the link's own moment is the boundary",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: planNow.Add(-day), IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("b", ttB, 2, missingFor(day+time.Millisecond)),
					synced("c", ttC, 3, missingFor(day)),
				},
			},
			want: want{clear: []string{"b"}, remove: []string{"c"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a stale strike is cleared even when the fetch proves nothing",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: planNow.Add(-day), IDs: []string{ttA}, Complete: false,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("b", ttB, 2, missingFor(3*day)),
					synced("c", ttC, 3, missingFor(time.Hour)),
				},
			},
			want: want{clear: []string{"b"}, prune: PruneSkippedIncomplete, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a stale strike on an item the user took over is cleared once",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: planNow.Add(-day), IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1, missingFor(3*day)),
					synced("b", ttB, 2, missingFor(3*day), status(models.WatchlistStatusWatching)),
				},
			},
			want: want{clear: []string{"a", "b"}, prune: PruneConsidered, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a worker sync that does not say which link it is for never marks or removes",
			in: Input{
				Mode: ModeSync, Now: planNow, LinkedAt: noLink, IDs: []string{ttA, ttC}, Complete: true, Resolved: abc,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1, missingFor(day)),
					synced("b", ttB, 2),
					synced("d", "tt0000004", 4, missingFor(9*day)),
				},
			},
			want: want{add: []string{ttC}, clear: []string{"a"}, prune: PruneSkippedNoLink, counts: [4]int{2, 1, 0, 0}},
		},

		// --- modes that never remove ---
		{
			name: "linking never marks or removes, and resets every mark",
			in: Input{
				Mode: ModeLink, Now: planNow, IDs: []string{ttA}, Complete: true,
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1),
					synced("b", ttB, 2),
					synced("c", ttC, 3, missingFor(9*day)),
				},
			},
			want: want{clear: []string{"c"}, prune: PruneSkippedMode, counts: [4]int{1, 1, 0, 0}},
		},
		{
			name: "a CSV import never marks, unmarks or removes",
			in: Input{
				Mode: ModeCSV, Now: planNow, IDs: []string{ttA, "tt0000004"}, Complete: true,
				Resolved: map[string]Target{"tt0000004": movie(4)},
				Existing: []*models.WatchlistItem{
					synced("a", ttA, 1, missingFor(9*day)),
					synced("b", ttB, 2),
					synced("c", ttC, 3, missingFor(9*day)),
				},
			},
			want: want{add: []string{"tt0000004"}, prune: PruneSkippedMode, counts: [4]int{2, 1, 0, 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.in.Now.IsZero() {
				tt.in.Now = planNow
			}
			switch {
			case tt.in.LinkedAt.Equal(noLink):
				tt.in.LinkedAt = time.Time{}
			case tt.in.LinkedAt.IsZero() && tt.in.Mode == ModeSync:
				tt.in.LinkedAt = planLinked
			}
			checkPlan(t, BuildPlan(tt.in), tt.want)
		})
	}
}

// Every removal carries the mark it was decided on and the updated_at the
// plan saw, which the store's delete then insists on: an item unmarked,
// re-marked or edited in between is not deleted.
func TestBuildPlanRemovalsCarryWhatThePlanSaw(t *testing.T) {
	gone := synced("b", ttB, 2, missingFor(3*day))
	gone.UpdatedAt = planNow.Add(-40 * day)
	plan := BuildPlan(Input{
		Mode: ModeSync, Now: planNow, LinkedAt: planLinked, IDs: []string{ttA}, Complete: true,
		Existing: []*models.WatchlistItem{synced("a", ttA, 1), gone},
	})
	want := []Removal{{ItemID: gone.ID, MissingSince: planNow.Add(-3 * day), UpdatedAt: planNow.Add(-40 * day)}}
	if !reflect.DeepEqual(plan.Remove, want) {
		t.Errorf("removals = %+v, want %+v", plan.Remove, want)
	}
}

func TestBuildPlanDoesNotChangeItsInput(t *testing.T) {
	existing := []*models.WatchlistItem{synced("a", ttA, 1), synced("b", ttB, 2, missingFor(day)), item("m", tmdbID(3))}
	before := make([]models.WatchlistItem, len(existing))
	for i, it := range existing {
		before[i] = *it
	}
	ids := []string{ttA, ttC, ttC}
	BuildPlan(Input{Mode: ModeSync, Now: planNow, LinkedAt: planLinked, IDs: ids, Complete: true, Existing: existing, Resolved: abc})
	for i, it := range existing {
		if !reflect.DeepEqual(*it, before[i]) {
			t.Errorf("item %d changed: %+v", i, *it)
		}
	}
	if !reflect.DeepEqual(ids, []string{ttA, ttC, ttC}) {
		t.Errorf("ids changed: %v", ids)
	}
}

// Outcomes are what the CSV import reports per row.
func TestBuildPlanOutcomes(t *testing.T) {
	plan := BuildPlan(Input{
		Mode: ModeCSV, Now: planNow, IDs: []string{ttA, ttB, ttC, "tt0000004", ttA},
		Existing:  []*models.WatchlistItem{synced("a", ttA, 1)},
		Resolved:  map[string]Target{ttB: movie(2)},
		Unmatched: map[string]bool{ttC: true},
	})
	want := map[string]Outcome{ttA: OutcomeExisting, ttB: OutcomeAdded, ttC: OutcomeUnmatched, "tt0000004": OutcomePending}
	if !reflect.DeepEqual(plan.Outcomes, want) {
		t.Errorf("outcomes = %v, want %v", plan.Outcomes, want)
	}
}
