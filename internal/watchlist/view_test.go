package watchlist

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
)

var base = time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

func offer(id int, name, access string) models.ProviderOffer {
	return models.ProviderOffer{ProviderID: id, ProviderName: name, LogoPath: "/" + name + ".jpg", Access: access}
}

func title(key string, rating float64, runtime int, offers ...models.ProviderOffer) *models.Title {
	fetched := base.Add(-6 * time.Hour)
	return &models.Title{
		ID: key,
		TitleMetadata: models.TitleMetadata{
			PosterPath: "/" + key + ".jpg", Rating: rating, Runtime: runtime,
			Genres: []string{"Drama"}, Overview: "About " + key,
		},
		Providers: map[string]models.RegionSnapshot{
			"US": {Link: "https://example.test/" + key, Offers: offers, FetchedAt: fetched},
			"GB": {Offers: []models.ProviderOffer{offer(337, "Disney Plus", models.AccessSubscription)}, FetchedAt: fetched},
		},
	}
}

// fixture is a user with Netflix and Hulu and a five-item watchlist:
//
//	Matrix    movie  planned   on Netflix (theirs); rentable on Amazon
//	Thrones   show   watching  on HBO Max only (not theirs)
//	Arrival   movie  finished  on Netflix and Hulu (both theirs)
//	Unlinked  movie  planned   no TMDb ID
//	Missing   movie  planned   TMDb ID without a stored title
//
// Newest first they are in that order.
type fixture struct {
	store *storetest.Store
	user  *models.User
}

func newFixture() *fixture {
	store := storetest.New()
	user := store.AddUser(&models.User{
		Email:  "viewer@example.com",
		Region: "US",
		Services: []models.ServiceSubscription{
			{Code: models.ServiceNetflix, Active: true},
			{Code: models.ServiceHulu, Active: true},
			{Code: models.ServiceMax, Active: false},
		},
	})
	add := func(name, typ, status string, year, tmdbID, daysAgo int) {
		store.AddItem(&models.WatchlistItem{
			UserID: user.ID, Title: name, Type: typ, Status: status, Year: year, TMDbID: tmdbID,
			AddedAt: base.AddDate(0, 0, -daysAgo), UpdatedAt: base,
		})
	}
	add("The Matrix", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 1999, 603, 1)
	add("Game of Thrones", models.WatchlistTypeShow, models.WatchlistStatusWatching, 2011, 1399, 2)
	add("Arrival", models.WatchlistTypeMovie, models.WatchlistStatusFinished, 2016, 329865, 3)
	add("Unlinked", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 0, 0, 4)
	add("Missing", models.WatchlistTypeMovie, models.WatchlistStatusPlanned, 2020, 999, 5)

	// Someone else's list must never leak in.
	other := store.AddUser(&models.User{Email: "other@example.com", Region: "US"})
	store.AddItem(&models.WatchlistItem{UserID: other.ID, Title: "Not Yours", Type: models.WatchlistTypeMovie, TMDbID: 603, AddedAt: base})

	store.PutTitle(title("movie:603", 8.2, 136,
		offer(8, "Netflix", models.AccessSubscription),
		offer(10, "Amazon Video", models.AccessRent),
		offer(10, "Amazon Video", models.AccessBuy),
	))
	store.PutTitle(title("tv:1399", 8.5, 60, offer(1899, "HBO Max", models.AccessSubscription)))
	store.PutTitle(title("movie:329865", 7.6, 116,
		offer(8, "Netflix", models.AccessSubscription),
		offer(15, "Hulu", models.AccessAds),
	))
	return &fixture{store: store, user: user}
}

func (f *fixture) list(t *testing.T, opts Options) *View {
	t.Helper()
	view, err := NewLister(f.store, nil, "US").List(context.Background(), f.user, opts)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return view
}

func titles(view *View) []string {
	names := []string{}
	for _, item := range view.Items {
		names = append(names, item.Title)
	}
	return names
}

func boolPtr(b bool) *bool { return &b }

func TestListAttachesMetadataAndAvailability(t *testing.T) {
	f := newFixture()
	view := f.list(t, Options{})

	if got, want := titles(view), []string{"The Matrix", "Game of Thrones", "Arrival", "Unlinked", "Missing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if view.Total != 5 || view.Region != "US" {
		t.Errorf("Total = %d, Region = %q", view.Total, view.Region)
	}

	matrix := view.Items[0]
	if matrix.PosterPath != "/movie:603.jpg" || matrix.Runtime != 136 || matrix.Rating != 8.2 || matrix.Overview == "" || len(matrix.Genres) != 1 {
		t.Errorf("metadata not attached: %+v", matrix)
	}
	want := &Availability{
		Region:    "US",
		CheckedAt: base.Add(-6 * time.Hour),
		Link:      "https://example.test/movie:603",
		Providers: []Provider{{Code: "netflix", Name: "Netflix", LogoPath: "/Netflix.jpg", Access: []string{"subscription"}}},
		Elsewhere: []Provider{{Name: "Amazon Video", LogoPath: "/Amazon Video.jpg", Access: []string{"rent", "buy"}}},
	}
	if !reflect.DeepEqual(matrix.Availability, want) {
		t.Errorf("availability =\n%+v\nwant\n%+v", matrix.Availability, want)
	}

	// On a service the user has switched off: known, but elsewhere.
	thrones := view.Items[1].Availability
	if len(thrones.Providers) != 0 || len(thrones.Elsewhere) != 1 || thrones.Elsewhere[0].Code != models.ServiceMax || thrones.Elsewhere[0].Name != "HBO Max" {
		t.Errorf("thrones availability = %+v", thrones)
	}

	for _, i := range []int{3, 4} {
		if view.Items[i].Availability != nil {
			t.Errorf("%s has availability %+v, want none", view.Items[i].Title, view.Items[i].Availability)
		}
	}
}

func TestListCostsTwoQueries(t *testing.T) {
	f := newFixture()
	for i := 0; i < 250; i++ {
		f.store.AddItem(&models.WatchlistItem{
			UserID: f.user.ID, Title: "Filler", Type: models.WatchlistTypeMovie, Status: models.WatchlistStatusPlanned,
			TMDbID: 10000 + i, AddedAt: base.AddDate(0, 0, -10),
		})
	}
	f.store.Queries = map[string]int{}

	view := f.list(t, Options{Status: models.WatchlistStatusPlanned, Sort: "-rating", Limit: 20})

	want := map[string]int{"ListWatchlistItems": 1, "GetTitles": 1}
	if !reflect.DeepEqual(f.store.Queries, want) {
		t.Errorf("queries = %v, want %v whatever the list size", f.store.Queries, want)
	}
	if view.Total != 253 || len(view.Items) != 20 {
		t.Errorf("Total = %d, returned %d", view.Total, len(view.Items))
	}
}

func TestListFilters(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"status", Options{Status: models.WatchlistStatusPlanned}, []string{"The Matrix", "Unlinked", "Missing"}},
		{"type", Options{Type: models.WatchlistTypeShow}, []string{"Game of Thrones"}},
		{"search ignores case", Options{Search: "MAT"}, []string{"The Matrix"}},
		{"search matches inside the title", Options{Search: " of "}, []string{"Game of Thrones"}},
		{"search with regex characters is literal", Options{Search: ".*"}, []string{}},
		{"service", Options{Services: []string{models.ServiceHulu}}, []string{"Arrival"}},
		{"any of several services", Options{Services: []string{models.ServiceHulu, models.ServiceMax}}, []string{"Game of Thrones", "Arrival"}},
		{"service the user does not have", Options{Services: []string{models.ServiceMax}}, []string{"Game of Thrones"}},
		{"service nothing streams on", Options{Services: []string{models.ServiceTubi}}, []string{}},
		{"available", Options{Available: boolPtr(true)}, []string{"The Matrix", "Arrival"}},
		{"unavailable includes unknown", Options{Available: boolPtr(false)}, []string{"Game of Thrones", "Unlinked", "Missing"}},
		{"status and available", Options{Status: models.WatchlistStatusPlanned, Available: boolPtr(true)}, []string{"The Matrix"}},
		{"type, service and search", Options{Type: models.WatchlistTypeMovie, Services: []string{models.ServiceNetflix}, Search: "arr"}, []string{"Arrival"}},
		{"contradictory filters", Options{Services: []string{models.ServiceMax}, Available: boolPtr(true)}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newFixture().list(t, tt.opts)
			if got := titles(view); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %v, want %v", got, tt.want)
			}
			if view.Total != len(tt.want) {
				t.Errorf("Total = %d, want %d", view.Total, len(tt.want))
			}
		})
	}
}

func TestListSort(t *testing.T) {
	tests := []struct {
		sort string
		want []string
	}{
		{"", []string{"The Matrix", "Game of Thrones", "Arrival", "Unlinked", "Missing"}},
		{"-added_at", []string{"The Matrix", "Game of Thrones", "Arrival", "Unlinked", "Missing"}},
		{"added_at", []string{"Missing", "Unlinked", "Arrival", "Game of Thrones", "The Matrix"}},
		{"title", []string{"Arrival", "Game of Thrones", "Missing", "The Matrix", "Unlinked"}},
		{"-title", []string{"Unlinked", "The Matrix", "Missing", "Game of Thrones", "Arrival"}},
		// Items without a value go last in either direction.
		{"year", []string{"The Matrix", "Game of Thrones", "Arrival", "Missing", "Unlinked"}},
		{"-year", []string{"Missing", "Arrival", "Game of Thrones", "The Matrix", "Unlinked"}},
		{"rating", []string{"Arrival", "The Matrix", "Game of Thrones", "Unlinked", "Missing"}},
		{"-rating", []string{"Game of Thrones", "The Matrix", "Arrival", "Unlinked", "Missing"}},
		{"runtime", []string{"Game of Thrones", "Arrival", "The Matrix", "Unlinked", "Missing"}},
		{"-runtime", []string{"The Matrix", "Arrival", "Game of Thrones", "Unlinked", "Missing"}},
		// Available first, newest first within each group.
		{"available", []string{"The Matrix", "Arrival", "Game of Thrones", "Unlinked", "Missing"}},
		{"-available", []string{"Game of Thrones", "Unlinked", "Missing", "The Matrix", "Arrival"}},
		{"status", []string{"Arrival", "The Matrix", "Unlinked", "Missing", "Game of Thrones"}},
		{"nonsense", []string{"The Matrix", "Game of Thrones", "Arrival", "Unlinked", "Missing"}},
	}
	for _, tt := range tests {
		t.Run("sort="+tt.sort, func(t *testing.T) {
			if got := titles(newFixture().list(t, Options{Sort: tt.sort})); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %v\nwant    %v", got, tt.want)
			}
		})
	}
}

func TestListPaging(t *testing.T) {
	tests := []struct {
		name          string
		limit, offset int
		want          []string
	}{
		{"first page", 2, 0, []string{"The Matrix", "Game of Thrones"}},
		{"second page", 2, 2, []string{"Arrival", "Unlinked"}},
		{"last, short page", 2, 4, []string{"Missing"}},
		{"offset past the end", 2, 9, []string{}},
		{"no limit", 0, 1, []string{"Game of Thrones", "Arrival", "Unlinked", "Missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view := newFixture().list(t, Options{Limit: tt.limit, Offset: tt.offset})
			if got := titles(view); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("items = %v, want %v", got, tt.want)
			}
			if view.Total != 5 {
				t.Errorf("Total = %d, want 5 regardless of the page", view.Total)
			}
		})
	}
}

func TestListFacets(t *testing.T) {
	allStatuses := map[string]int{"planned": 3, "watching": 1, "finished": 1}

	tests := []struct {
		name string
		opts Options
		want Facets
	}{
		{
			name: "whole list",
			opts: Options{},
			want: Facets{Services: map[string]int{"netflix": 2, "hulu": 1, "max": 1}, Available: 2, Unavailable: 3, Status: allStatuses},
		},
		{
			// The status counts keep describing the other tabs; the rest
			// describes the selected one.
			name: "status filter",
			opts: Options{Status: models.WatchlistStatusPlanned},
			want: Facets{Services: map[string]int{"netflix": 1}, Available: 1, Unavailable: 2, Status: allStatuses},
		},
		{
			name: "availability and service filters do not change the counts",
			opts: Options{Available: boolPtr(true), Services: []string{models.ServiceHulu}},
			want: Facets{Services: map[string]int{"netflix": 2, "hulu": 1, "max": 1}, Available: 2, Unavailable: 3, Status: allStatuses},
		},
		{
			name: "type filter narrows everything",
			opts: Options{Type: models.WatchlistTypeMovie},
			want: Facets{Services: map[string]int{"netflix": 2, "hulu": 1}, Available: 2, Unavailable: 2, Status: map[string]int{"planned": 3, "finished": 1}},
		},
		{
			name: "search narrows everything",
			opts: Options{Search: "thrones"},
			want: Facets{Services: map[string]int{"max": 1}, Available: 0, Unavailable: 1, Status: map[string]int{"watching": 1}},
		},
		{
			name: "paging does not change the counts",
			opts: Options{Limit: 1, Offset: 3},
			want: Facets{Services: map[string]int{"netflix": 2, "hulu": 1, "max": 1}, Available: 2, Unavailable: 3, Status: allStatuses},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newFixture().list(t, tt.opts).Facets; !reflect.DeepEqual(got, tt.want) {
				t.Errorf("facets = %+v\nwant     %+v", got, tt.want)
			}
		})
	}
}

func TestListUsesTheUsersRegion(t *testing.T) {
	f := newFixture()
	f.user.Region = "GB"
	f.user.Services = []models.ServiceSubscription{{Code: models.ServiceDisneyPlus, Active: true}}

	view := f.list(t, Options{Available: boolPtr(true)})
	if view.Region != "GB" || len(view.Items) != 3 {
		t.Fatalf("Region = %q, %d available items; want GB and the three stored titles", view.Region, len(view.Items))
	}
	if av := view.Items[0].Availability; av.Region != "GB" || av.Providers[0].Code != models.ServiceDisneyPlus {
		t.Errorf("availability = %+v", av)
	}

	f.user.Region = "" // falls back to the default region
	if view := f.list(t, Options{}); view.Region != "US" {
		t.Errorf("Region = %q for a user without one, want the default", view.Region)
	}
}

// fakeEnsurer resolves the titles it knows and records what it was asked for.
type fakeEnsurer struct {
	known map[string]*models.Title
	asked []string
	wait  time.Duration
}

func (e *fakeEnsurer) Ensure(ctx context.Context, refs []models.TitleRef, _ map[string]*models.Title, _ string) map[string]*models.Title {
	resolved := make(map[string]*models.Title)
	for _, ref := range refs {
		e.asked = append(e.asked, ref.Key())
	}
	if e.wait > 0 {
		select {
		case <-ctx.Done():
			return resolved // out of budget: nothing resolved
		case <-time.After(e.wait):
		}
	}
	for _, ref := range refs {
		if t, ok := e.known[ref.Key()]; ok {
			resolved[ref.Key()] = t
		}
	}
	return resolved
}

func TestListFetchesTitlesWithoutASnapshot(t *testing.T) {
	f := newFixture()
	ensurer := &fakeEnsurer{known: map[string]*models.Title{
		"movie:999": title("movie:999", 6.1, 95, offer(15, "Hulu", models.AccessSubscription)),
	}}

	view, err := NewLister(f.store, ensurer, "US").List(context.Background(), f.user, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"movie:999"}; !reflect.DeepEqual(ensurer.asked, want) {
		t.Errorf("ensurer was asked for %v, want only the missing %v", ensurer.asked, want)
	}
	missing := view.Items[4]
	if missing.Availability == nil || len(missing.Availability.Providers) != 1 || missing.Rating != 6.1 {
		t.Errorf("the fetched title was not attached: %+v", missing)
	}
	if view.Facets.Available != 3 {
		t.Errorf("Facets.Available = %d, want the fetched title counted", view.Facets.Available)
	}
}

func TestListGivesUpOnSlowFetches(t *testing.T) {
	f := newFixture()
	ensurer := &fakeEnsurer{wait: time.Minute, known: map[string]*models.Title{"movie:999": title("movie:999", 6.1, 95)}}
	lister := NewLister(f.store, ensurer, "US")
	lister.missBudget = 20 * time.Millisecond

	start := time.Now()
	view, err := lister.List(context.Background(), f.user, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("List took %v, want it bounded by the miss budget", elapsed)
	}
	if len(view.Items) != 5 || view.Items[4].Availability != nil {
		t.Errorf("unresolved title should be listed without availability: %+v", view.Items[4])
	}
	if view.Timings.TMDb < lister.missBudget {
		t.Errorf("Timings.TMDb = %v, want at least the budget spent", view.Timings.TMDb)
	}
}

func TestListDoesNotFetchWhenEverythingIsStored(t *testing.T) {
	f := newFixture()
	f.store.PutTitle(title("movie:999", 6.1, 95))
	ensurer := &fakeEnsurer{}
	if _, err := NewLister(f.store, ensurer, "US").List(context.Background(), f.user, Options{}); err != nil {
		t.Fatal(err)
	}
	if len(ensurer.asked) != 0 {
		t.Errorf("ensurer was asked for %v although every title is stored", ensurer.asked)
	}
}

func TestListStoreErrors(t *testing.T) {
	f := newFixture()
	f.store.Err = errors.New("database down")
	if _, err := NewLister(f.store, nil, "US").List(context.Background(), f.user, Options{}); err == nil {
		t.Error("List succeeded without a database")
	}
}

func TestListEmptyWatchlist(t *testing.T) {
	store := storetest.New()
	user := store.AddUser(&models.User{Email: "new@example.com"})
	view, err := NewLister(store, nil, "US").List(context.Background(), user, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if view.Items == nil || len(view.Items) != 0 || view.Total != 0 {
		t.Errorf("Items = %#v, Total = %d; want an empty, non-nil list", view.Items, view.Total)
	}
	if raw, _ := json.Marshal(view.Items); string(raw) != "[]" {
		t.Errorf("empty list encodes as %s, want []", raw)
	}
}

// Clients written before availability was embedded read these keys.
func TestItemJSONKeepsTheOriginalFields(t *testing.T) {
	view := newFixture().list(t, Options{})

	decode := func(item Item) map[string]any {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		return fields
	}

	matrix := decode(view.Items[0])
	for _, key := range []string{"id", "user_id", "title", "type", "year", "tmdb_id", "status", "added_at", "updated_at"} {
		if _, ok := matrix[key]; !ok {
			t.Errorf("item JSON lost the %q field: %v", key, matrix)
		}
	}
	for _, key := range []string{"poster_path", "runtime", "genres", "rating", "overview", "availability"} {
		if _, ok := matrix[key]; !ok {
			t.Errorf("item JSON lacks the new %q field", key)
		}
	}
	if matrix["id"] != view.Items[0].ID.Hex() || matrix["title"] != "The Matrix" || matrix["type"] != "movie" {
		t.Errorf("original fields changed shape: %v", matrix)
	}
	availability := matrix["availability"].(map[string]any)
	for _, key := range []string{"region", "checked_at", "link", "providers", "elsewhere"} {
		if _, ok := availability[key]; !ok {
			t.Errorf("availability JSON lacks %q", key)
		}
	}

	unlinked := decode(view.Items[3])
	if value, ok := unlinked["availability"]; !ok || value != nil {
		t.Errorf("an item without availability should carry \"availability\": null, got %v (present: %v)", value, ok)
	}
}

func TestBuildAvailability(t *testing.T) {
	snap := models.RegionSnapshot{
		Link:      "https://example.test/watch",
		FetchedAt: base,
		Offers: []models.ProviderOffer{
			offer(2303, "Paramount Plus Premium", models.AccessSubscription),
			offer(2616, "Paramount Plus Essential", models.AccessSubscription),
			offer(999010, "Paramount+ Amazon Channel", models.AccessSubscription),
			offer(73, "Tubi TV", models.AccessAds),
			offer(73, "Tubi TV", models.AccessFree),
			offer(8, "Netflix", models.AccessRent), // a service the user has, but only to rent
			offer(2, "Apple TV Store", models.AccessBuy),
			offer(2, "Apple TV Store", models.AccessRent),
			offer(15, "Hulu", models.AccessSubscription),
		},
	}
	active := map[string]bool{models.ServiceParamountPlus: true, models.ServiceTubi: true, models.ServiceNetflix: true, models.ServiceDisneyPlus: true}

	got := BuildAvailability(snap, "US", active)

	wantProviders := []Provider{
		{Code: "paramount_plus", Name: "Paramount+", LogoPath: "/Paramount Plus Premium.jpg", Access: []string{"subscription"}},
		{Code: "tubi", Name: "Tubi", LogoPath: "/Tubi TV.jpg", Access: []string{"free", "ads"}},
	}
	if !reflect.DeepEqual(got.Providers, wantProviders) {
		t.Errorf("Providers =\n%+v\nwant\n%+v", got.Providers, wantProviders)
	}
	wantElsewhere := []Provider{
		// Streaming offers first, then stores; by name within each.
		{Code: "hulu", Name: "Hulu", LogoPath: "/Hulu.jpg", Access: []string{"subscription"}},
		{Name: "Paramount+ Amazon Channel", LogoPath: "/Paramount+ Amazon Channel.jpg", Access: []string{"subscription"}},
		{Name: "Apple TV Store", LogoPath: "/Apple TV Store.jpg", Access: []string{"rent", "buy"}},
		{Code: "netflix", Name: "Netflix", LogoPath: "/Netflix.jpg", Access: []string{"rent"}},
	}
	if !reflect.DeepEqual(got.Elsewhere, wantElsewhere) {
		t.Errorf("Elsewhere =\n%+v\nwant\n%+v", got.Elsewhere, wantElsewhere)
	}
	if want := []string{"disney_plus", "netflix"}; !reflect.DeepEqual(got.Unmatched(active), want) {
		t.Errorf("Unmatched = %v, want %v", got.Unmatched(active), want)
	}

	empty := BuildAvailability(models.RegionSnapshot{FetchedAt: base}, "US", active)
	if empty.Providers == nil || empty.Elsewhere == nil {
		t.Error("an empty snapshot should produce empty, non-nil provider lists")
	}
	var none *Availability
	if got := none.Unmatched(map[string]bool{"netflix": true, "hulu": false}); !reflect.DeepEqual(got, []string{"netflix"}) {
		t.Errorf("nil availability Unmatched = %v", got)
	}
}
