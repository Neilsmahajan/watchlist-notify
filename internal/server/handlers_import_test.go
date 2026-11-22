package server

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// csvOf builds an IMDb export: the columns IMDb writes, one row per ID.
func csvOf(ids ...string) string {
	var b strings.Builder
	b.WriteString("Position,Const,Created,Modified,Description,Title,Title Type\n")
	for i, id := range ids {
		fmt.Fprintf(&b, "%d,%s,2024-01-01,2024-01-01,,\"A Title, With a Comma\",Movie\n", i+1, id)
	}
	return b.String()
}

// have gives the viewer watchlist items for the IDs, as an earlier import
// would have left them.
func (h *syncHarness) have(ids ...string) {
	for i, id := range ids {
		h.store.AddItem(&models.WatchlistItem{
			UserID: h.user.ID, Title: "Have " + id, Type: models.WatchlistTypeMovie, TMDbID: 500000 + i, IMDbID: id,
			Status: models.WatchlistStatusWatching, AddedAt: base, UpdatedAt: base,
		})
	}
}

func rowErrors(body map[string]any) []string {
	out := []string{}
	raw, _ := body["errors"].([]any)
	for _, e := range raw {
		entry := e.(map[string]any)
		out = append(out, fmt.Sprintf("%v: %v", entry["row"], entry["reason"]))
	}
	return out
}

// Rows the user already has are settled from one read of their watchlist.
func TestImportDuplicatesCostNoLookups(t *testing.T) {
	h := newSyncHarness(t)
	ids := ttIDs(100, 200)
	h.have(ids...)

	body := h.json(h.upload(csvOf(ids...)), http.StatusOK)

	if body["imported"] != 0.0 || body["duplicates"] != 200.0 || len(rowErrors(body)) != 0 {
		t.Errorf("response = %v, want 200 duplicates and nothing else", body)
	}
	if _, isArray := body["errors"].([]any); !isArray {
		t.Errorf("errors = %v, want an empty array", body["errors"])
	}
	if got := h.finder.lookups(); len(got) != 0 {
		t.Errorf("TMDb was asked about %d titles the user already has", len(got))
	}
	if h.titles.calls != 0 {
		t.Errorf("availability was fetched %d times for an import that added nothing", h.titles.calls)
	}
	if n := h.store.Queries["ListWatchlistItems"]; n != 1 {
		t.Errorf("the watchlist was read %d times, want once for the whole file", n)
	}
	if n := h.store.Queries["CreateWatchlistItem"] + h.store.Queries["CreateWatchlistItems"]; n != 0 {
		t.Errorf("%d inserts were attempted for rows that already exist", n)
	}
	for _, item := range h.store.Items(h.user.ID) {
		if item.Status != models.WatchlistStatusWatching || item.Source != "" {
			t.Fatalf("an import changed an existing item: %+v", item)
		}
	}
}

// New rows cost one lookup each, made concurrently, and one insert in all.
func TestImportNewRowsCostOneConcurrentLookupEach(t *testing.T) {
	h := newSyncHarness(t)
	h.have(ttIDs(100, 30)...)
	h.finder.delay = 20 * time.Millisecond
	fresh := ttIDs(2000, 40)

	body := h.json(h.upload(csvOf(append(ttIDs(100, 30), fresh...)...)), http.StatusOK)

	if body["imported"] != 40.0 || body["duplicates"] != 30.0 || len(rowErrors(body)) != 0 {
		t.Errorf("response = %v, want 40 imported and 30 duplicates", body)
	}
	if got := h.finder.lookups(); !reflect.DeepEqual(got, fresh) {
		t.Errorf("TMDb was asked about %d titles, want exactly the 40 new ones", len(got))
	}
	if h.finder.maxSeen < 2 || h.finder.maxSeen > 8 {
		t.Errorf("at most %d lookups were in flight, want more than one and no more than 8", h.finder.maxSeen)
	}
	if n := h.store.Queries["CreateWatchlistItems"]; n != 1 {
		t.Errorf("rows were inserted in %d batches, want 1", n)
	}
	// New rows are marked as imported, planned, with TMDb's metadata.
	added := h.items()[fresh[0]]
	if added == nil || added.Source != models.SourceCSV || added.Status != models.WatchlistStatusPlanned || added.TMDbID != 2000 || added.Title != "Movie 2000" || added.Year != 2001 {
		t.Errorf("imported item = %+v", added)
	}
	// Availability baselines are fetched for the new titles only.
	if h.titles.calls != 40 {
		t.Errorf("availability fetched for %d titles, want the 40 new ones", h.titles.calls)
	}
}

// When TMDb is too slow for the whole file, the rows that were resolved are
// stored, the rest are reported, and uploading again finishes the job.
func TestImportDeadlineLeavesAConsistentPartialResult(t *testing.T) {
	h := newSyncHarness(t)
	ids := ttIDs(3000, 64)
	// TMDb answers for the first 16 rows and not at all for the other 48.
	h.finder.hang = map[string]bool{}
	for _, id := range ids[16:] {
		h.finder.hang[id] = true
	}
	h.server.resolveBudget = 300 * time.Millisecond

	body := h.json(h.upload(csvOf(ids...)), http.StatusOK)

	imported := int(body["imported"].(float64))
	if imported != 16 || body["duplicates"] != 0.0 {
		t.Fatalf("response = %v, want the 16 rows TMDb answered for imported", body)
	}
	if stored := len(h.items()); stored != imported {
		t.Errorf("%d items stored but %d reported imported", stored, imported)
	}
	errs := rowErrors(body)
	if len(errs) != 1 || !strings.Contains(errs[0], fmt.Sprintf("with %d titles left", 64-imported)) || !strings.Contains(errs[0], "upload the file again") {
		t.Errorf("errors = %v, want one entry accounting for the %d rows left", errs, 64-imported)
	}
	// Every stored item is whole: nothing half-written.
	for id, item := range h.items() {
		if item.TMDbID == 0 || item.Title == "" || item.Source != models.SourceCSV {
			t.Errorf("%s was stored incomplete: %+v", id, item)
		}
	}

	// Uploading the same file again continues where it stopped.
	h.finder.hang = nil
	h.finder.reset()
	h.server.resolveBudget = 0
	again := h.json(h.upload(csvOf(ids...)), http.StatusOK)
	if int(again["imported"].(float64)) != 64-imported || int(again["duplicates"].(float64)) != imported || len(rowErrors(again)) != 0 {
		t.Errorf("second upload = %v, want %d imported and %d duplicates", again, 64-imported, imported)
	}
	if got := len(h.finder.lookups()); got != 64-imported {
		t.Errorf("second upload made %d lookups, want %d", got, 64-imported)
	}
	if len(h.items()) != 64 {
		t.Errorf("%d items after two uploads, want 64", len(h.items()))
	}
}

// The response keeps the shape the frontend reads: imported, duplicates and
// errors[{row, reason}], with rows numbered from the header.
func TestImportResponseShape(t *testing.T) {
	h := newSyncHarness(t)
	h.have(ttID(100))
	h.finder.errs[ttID(7)] = errors.New("tmdb: status 500")
	file := "\ufeffConst,Your Rating,Title\n" + // row 1, with a byte order mark
		ttID(1) + ",8,New\n" + // row 2: imported
		ttID(100) + ",,Have\n" + // row 3: duplicate
		",,No ID\n" + // row 4
		"nm0000206,,A person\n" + // row 5
		unknownTitle + ",,Unknown to TMDb\n" + // row 6
		ttID(1) + ",,Same as row 2\n" + // row 7: duplicate within the file
		ttID(7) + ",,TMDb fails\n" + // row 8
		"  " + ttID(2) + "  ,,Padded\n" // row 9: imported

	rec := h.upload(file)
	body := h.json(rec, http.StatusOK)

	if got := sortedKeys(body); !reflect.DeepEqual(got, []string{"duplicates", "errors", "imported"}) {
		t.Errorf("response fields = %v", got)
	}
	if body["imported"] != 2.0 || body["duplicates"] != 2.0 {
		t.Errorf("imported = %v, duplicates = %v; want 2 and 2", body["imported"], body["duplicates"])
	}
	want := []string{
		"4: missing IMDb ID",
		"5: not an IMDb title ID (tt…)",
		"6: no tmdb match for imdb id " + unknownTitle,
		"8: could not be imported this time; upload the file again to retry",
	}
	if got := rowErrors(body); !reflect.DeepEqual(got, want) {
		t.Errorf("errors = %q\nwant %q", got, want)
	}
	if strings.Contains(rec.Body.String(), "status 500") {
		t.Errorf("an upstream error reached the response: %s", rec.Body)
	}
}

func TestImportRejectsFilesItCannotUse(t *testing.T) {
	tests := map[string]struct {
		file string
		want string
	}{
		"no Const column":     {"Title,Year\nThe Matrix,1999\n", "missing required column: Const"},
		"nothing importable":  {"Const\nnm0000206\n\"\"\n", "no rows imported"},
		"only unknown titles": {csvOf(unknownTitle), "no rows imported"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := newSyncHarness(t)
			body := h.json(h.upload(tt.file), http.StatusBadRequest)
			if body["error"] != tt.want {
				t.Errorf("error = %v, want %q", body["error"], tt.want)
			}
			if len(h.items()) != 0 {
				t.Errorf("a rejected file added items")
			}
		})
	}

	t.Run("empty file", func(t *testing.T) {
		h := newSyncHarness(t)
		h.json(h.upload(""), http.StatusBadRequest)
	})
	t.Run("no file", func(t *testing.T) {
		h := newSyncHarness(t)
		h.json(h.request(http.MethodPost, "/api/watchlist/import", ""), http.StatusBadRequest)
	})
	t.Run("header only", func(t *testing.T) {
		h := newSyncHarness(t)
		body := h.json(h.upload("Const,Title\n"), http.StatusOK)
		if body["imported"] != 0.0 || body["duplicates"] != 0.0 {
			t.Errorf("response = %v", body)
		}
	})
}

func TestImportUnavailableWithoutTMDb(t *testing.T) {
	h := newSyncHarness(t)
	h.build(nil)
	body := h.json(h.upload(csvOf(ttID(1))), http.StatusServiceUnavailable)
	if body["error"] != "import unavailable" {
		t.Errorf("error = %v", body["error"])
	}
}

// An import adds and does nothing else: it does not take over items the
// IMDb sync manages, and it never removes.
func TestImportLeavesSyncedItemsAlone(t *testing.T) {
	h := newSyncHarness(t)
	gone := time.Now().Add(-72 * time.Hour)
	h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: "Synced, missing", Type: models.WatchlistTypeMovie, TMDbID: 50, IMDbID: ttID(50),
		Status: models.WatchlistStatusPlanned, Source: models.SourceIMDb, IMDbMissingSince: &gone, AddedAt: base, UpdatedAt: base,
	})
	h.store.AddItem(&models.WatchlistItem{
		UserID: h.user.ID, Title: "Synced", Type: models.WatchlistTypeMovie, TMDbID: 51, IMDbID: ttID(51),
		Status: models.WatchlistStatusPlanned, Source: models.SourceIMDb, AddedAt: base, UpdatedAt: base,
	})

	body := h.json(h.upload(csvOf(ttID(51), ttID(52))), http.StatusOK)

	if body["imported"] != 1.0 || body["duplicates"] != 1.0 {
		t.Errorf("response = %v", body)
	}
	items := h.items()
	if len(items) != 3 {
		t.Fatalf("watchlist has %d items, want 3", len(items))
	}
	if it := items[ttID(50)]; it.IMDbMissingSince == nil || it.Source != models.SourceIMDb {
		t.Errorf("the import touched an item the sync manages: %+v", it)
	}
	if it := items[ttID(51)]; it.Source != models.SourceIMDb {
		t.Errorf("the import took over a synced item: %+v", it)
	}
	if it := items[ttID(52)]; it.Source != models.SourceCSV {
		t.Errorf("the new item's source = %q", it.Source)
	}
}

func TestParseIMDbCSVCapsTheNumberOfTitles(t *testing.T) {
	var b strings.Builder
	b.WriteString("Const\n")
	for i := 0; i < maxImportTitles+3; i++ {
		b.WriteString(ttID(i+1) + "\n")
	}
	b.WriteString(ttID(1) + "\n") // a repeat of a row that was accepted

	rows, rowErrs, err := parseIMDbCSV(strings.NewReader(b.String()))

	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != maxImportTitles+1 {
		t.Errorf("%d rows accepted, want %d titles and the repeat", len(rows), maxImportTitles)
	}
	if len(rowErrs) != 1 || rowErrs[0].Row != maxImportTitles+2 || !strings.Contains(rowErrs[0].Reason, "only the first") {
		t.Errorf("errors = %+v, want one entry at the first row over the limit", rowErrs)
	}
}
