package models

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestUserJSONCarriesTheIMDbLinkButNotTheThrottle(t *testing.T) {
	at := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)

	unlinked, err := json.Marshal(User{Email: "viewer@example.com", IMDbAttemptAt: &at})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(unlinked), "imdb") {
		t.Errorf("a user without a link serializes IMDb fields: %s", unlinked)
	}

	linked, err := json.Marshal(User{IMDbAttemptAt: &at, IMDbSync: &IMDbSync{
		SourceID: "ls042518938", LinkedAt: at, LastAttemptAt: &at, LastStatus: IMDbStatusOK, Total: 242,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Sync map[string]any `json:"imdb_sync"`
	}
	if err := json.Unmarshal(linked, &body); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"source_id", "linked_at", "last_attempt_at", "last_status", "total", "added", "removed", "unmatched"} {
		if _, ok := body.Sync[field]; !ok {
			t.Errorf("imdb_sync has no %q: %s", field, linked)
		}
	}
	// Unset optional fields are left out rather than sent as null.
	for _, field := range []string{"last_synced_at", "last_error"} {
		if _, ok := body.Sync[field]; ok {
			t.Errorf("imdb_sync carries an unset %q: %s", field, linked)
		}
	}
	if strings.Contains(string(linked), "imdb_attempt_at") {
		t.Errorf("the fetch throttle is exposed: %s", linked)
	}
}

func TestSyncOwned(t *testing.T) {
	owned := WatchlistItem{Source: SourceIMDb, Status: WatchlistStatusPlanned, IMDbID: "tt0133093"}
	tests := []struct {
		name string
		edit func(*WatchlistItem)
		want bool
	}{
		{"as the sync added it", func(*WatchlistItem) {}, true},
		{"added by hand", func(w *WatchlistItem) { w.Source = "" }, false},
		{"imported from a CSV", func(w *WatchlistItem) { w.Source = SourceCSV }, false},
		{"status changed", func(w *WatchlistItem) { w.Status = WatchlistStatusWatching }, false},
		{"finished", func(w *WatchlistItem) { w.Status = WatchlistStatusFinished }, false},
		{"tagged", func(w *WatchlistItem) { w.Tags = []string{"date night"} }, false},
		{"no IMDb ID to compare", func(w *WatchlistItem) { w.IMDbID = "" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := owned
			tt.edit(&item)
			if got := item.SyncOwned(); got != tt.want {
				t.Errorf("SyncOwned() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAdoptable(t *testing.T) {
	imported := WatchlistItem{Source: SourceCSV, Status: WatchlistStatusPlanned, IMDbID: "tt0133093"}
	tests := []struct {
		name string
		edit func(*WatchlistItem)
		want bool
	}{
		{"as a CSV import left it", func(*WatchlistItem) {}, true},
		{"imported before items had a source", func(w *WatchlistItem) { w.Source = "" }, true},
		{"added by hand: no IMDb ID", func(w *WatchlistItem) { w.Source, w.IMDbID = "", "" }, false},
		{"a CSV row without an IMDb ID", func(w *WatchlistItem) { w.IMDbID = "" }, false},
		{"already the sync's", func(w *WatchlistItem) { w.Source = SourceIMDb }, false},
		{"a source nobody knows", func(w *WatchlistItem) { w.Source = "letterboxd" }, false},
		{"status changed", func(w *WatchlistItem) { w.Status = WatchlistStatusWatching }, false},
		{"finished", func(w *WatchlistItem) { w.Status = WatchlistStatusFinished }, false},
		{"tagged", func(w *WatchlistItem) { w.Tags = []string{"date night"} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := imported
			tt.edit(&item)
			if got := item.Adoptable(); got != tt.want {
				t.Errorf("Adoptable() = %v, want %v", got, tt.want)
			}
			if item.Adoptable() && item.SyncOwned() {
				t.Error("an item is both adoptable and already the sync's")
			}
		})
	}
}
