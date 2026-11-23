package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/server"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var now = time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)

// fakeStore is an in-memory store with the semantics of the real one that
// matter here: upsert by address, defaults on insert, creation stamped with
// the current time, and a uniqueness rule per user and title.
type fakeStore struct {
	users  map[string]*models.User
	items  []*models.WatchlistItem
	titles map[string]*models.Title
	writes int
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]*models.User{}, titles: map[string]*models.Title{}}
}

func (f *fakeStore) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	if u, ok := f.users[email]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeStore) UpsertUser(_ context.Context, u *models.User) error {
	f.writes++
	existing, ok := f.users[u.Email]
	if !ok {
		existing = &models.User{ID: primitive.NewObjectID(), Email: u.Email, Preferences: models.Preferences{UseAccountEmail: true}}
		f.users[u.Email] = existing
	}
	existing.Name, existing.Region = u.Name, u.Region
	return nil
}

func (f *fakeStore) byID(id primitive.ObjectID) *models.User {
	for _, u := range f.users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func (f *fakeStore) UpdateServices(_ context.Context, id primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error) {
	f.writes++
	u := f.byID(id)
	if u == nil {
		return nil, errors.New("no such user")
	}
	for i := range services {
		if services[i].AddedAt.IsZero() {
			services[i].AddedAt = now
		}
	}
	u.Services = services
	return u, nil
}

func (f *fakeStore) UpdateUserPreferences(_ context.Context, id primitive.ObjectID, fields map[string]any) (*models.User, error) {
	f.writes++
	u := f.byID(id)
	if u == nil {
		return nil, errors.New("no such user")
	}
	for field, value := range fields {
		switch field {
		case "preferences.digest.enabled":
			u.Preferences.Digest.Enabled = value.(bool)
		case "preferences.digest_consent":
			u.Preferences.DigestConsent = value.(bool)
		case "preferences.marketing_consent":
			u.Preferences.MarketingConsent = value.(bool)
		case "preferences.use_account_email":
			u.Preferences.UseAccountEmail = value.(bool)
		case "preferences.notify_email":
			u.Preferences.NotifyEmail = value.(string)
		default:
			return nil, errors.New("unexpected preference field " + field)
		}
	}
	return u, nil
}

func (f *fakeStore) ListWatchlistItems(_ context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error) {
	var out []*models.WatchlistItem
	for _, item := range f.items {
		if item.UserID == opts.UserID {
			cp := *item
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateWatchlistItem(_ context.Context, item *models.WatchlistItem) error {
	for _, existing := range f.items {
		if existing.UserID == item.UserID && existing.Type == item.Type && existing.TMDbID == item.TMDbID {
			return database.ErrDuplicateWatchlistItem
		}
	}
	f.writes++
	item.ID = primitive.NewObjectID()
	item.AddedAt, item.UpdatedAt = now, now
	cp := *item
	f.items = append(f.items, &cp)
	return nil
}

func (f *fakeStore) UpdateWatchlistItem(_ context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error) {
	for _, item := range f.items {
		if item.ID == itemID && item.UserID == userID {
			f.writes++
			for field, value := range fields {
				if field != "added_at" {
					return nil, errors.New("unexpected item field " + field)
				}
				item.AddedAt = value.(time.Time)
			}
			return item, nil
		}
	}
	return nil, database.ErrWatchlistItemNotFound
}

func (f *fakeStore) GetTitles(_ context.Context, keys []string, _ []string) (map[string]*models.Title, error) {
	out := make(map[string]*models.Title)
	for _, key := range keys {
		if t, ok := f.titles[key]; ok {
			out[key] = t
		}
	}
	return out, nil
}

// fakeEnsurer stores a baseline for every title it is asked about, except
// the ones in fail.
type fakeEnsurer struct {
	store *fakeStore
	fail  map[string]bool
	calls int
}

func (e *fakeEnsurer) Ensure(_ context.Context, refs []models.TitleRef, stored map[string]*models.Title, region string) map[string]*models.Title {
	e.calls++
	resolved := make(map[string]*models.Title)
	for _, ref := range refs {
		if t, ok := stored[ref.Key()]; ok {
			resolved[ref.Key()] = t
			continue
		}
		if e.fail[ref.Key()] {
			continue
		}
		t := &models.Title{ID: ref.Key(), Providers: map[string]models.RegionSnapshot{region: {FetchedAt: now}}}
		e.store.titles[ref.Key()] = t
		e.store.writes++
		resolved[ref.Key()] = t
	}
	return resolved
}

func mustTitles(t *testing.T) []seedTitle {
	t.Helper()
	titles, err := parseTitles(titlesCSV)
	if err != nil {
		t.Fatal(err)
	}
	return titles
}

func TestEmbeddedList(t *testing.T) {
	titles := mustTitles(t)
	if len(titles) != 76 {
		t.Errorf("%d titles, want 76", len(titles))
	}
	counts := map[string]int{}
	imdb := map[string]bool{}
	for _, title := range titles {
		counts[title.Type]++
		counts[title.Status]++
		if imdb[title.IMDbID] {
			t.Errorf("duplicate IMDb ID %s", title.IMDbID)
		}
		imdb[title.IMDbID] = true
	}
	if counts["movie"] != 50 || counts["show"] != 26 {
		t.Errorf("movies/shows = %d/%d, want 50/26", counts["movie"], counts["show"])
	}
	if counts["planned"] <= counts["watching"]+counts["finished"] || counts["watching"] == 0 || counts["finished"] == 0 {
		t.Errorf("statuses = %v, want mostly planned with a few watching and finished", counts)
	}
}

func TestParseTitlesRejectsBadRows(t *testing.T) {
	header := "type,tmdb_id,imdb_id,year,title,status\n"
	for name, body := range map[string]string{
		"empty":            "",
		"bad type":         "series,1,tt1,2000,X,planned\n",
		"bad id":           "movie,zero,tt1,2000,X,planned\n",
		"bad status":       "movie,1,tt1,2000,X,done\n",
		"missing imdb":     "movie,1,,2000,X,planned\n",
		"duplicate":        "movie,1,tt1,2000,X,planned\nmovie,1,tt2,2000,Y,planned\n",
		"too few columns":  "movie,1,tt1,2000,X\n",
		"non-numeric year": "movie,1,tt1,soon,X,planned\n",
	} {
		if _, err := parseTitles([]byte(header + body)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSeedCreatesTheDemoAccount(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	fetch := &fakeEnsurer{store: db}

	rep, err := seed(context.Background(), db, fetch, titles, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.UserCreated || rep.ItemsInserted != 76 || rep.ItemsExisting != 0 || rep.TitlesFetched != 76 || rep.TitlesStored != 0 || rep.TitlesUnresolved != 0 {
		t.Errorf("report = %+v", rep)
	}

	user := db.users[server.DemoUserEmail]
	if user == nil {
		t.Fatal("no demo user")
	}
	if user.Name != "Demo" || user.Region != "US" {
		t.Errorf("user = %q in %q", user.Name, user.Region)
	}
	active := user.ActiveServiceCodes()
	if len(user.Services) != 6 || len(active) != 6 {
		t.Errorf("services = %+v", user.Services)
	}
	for _, code := range []string{"netflix", "max", "hulu", "disney_plus", "prime_video", "apple_tv_plus"} {
		if !active[code] {
			t.Errorf("service %s is not active", code)
		}
	}
	// The digest task selects users with digests enabled and consented to.
	if p := user.Preferences; p.Digest.Enabled || p.DigestConsent || p.MarketingConsent || !p.UseAccountEmail || p.NotifyEmail != "" {
		t.Errorf("preferences = %+v, want digests off and no consents", p)
	}

	if len(db.items) != 76 {
		t.Fatalf("%d items, want 76", len(db.items))
	}
	seenTimes := map[time.Time]bool{}
	statuses := map[string]int{}
	for _, item := range db.items {
		if item.UserID != user.ID {
			t.Errorf("%s belongs to another user", item.Title)
		}
		if item.TMDbID == 0 || item.IMDbID == "" || item.Year == 0 {
			t.Errorf("%s is missing IDs or year: %+v", item.Title, item)
		}
		if !item.AddedAt.Before(now) || item.AddedAt.Before(now.AddDate(0, -6, 0)) {
			t.Errorf("%s added at %s, want within the months before now", item.Title, item.AddedAt)
		}
		if seenTimes[item.AddedAt] {
			t.Errorf("%s shares its added_at with another item", item.Title)
		}
		seenTimes[item.AddedAt] = true
		statuses[item.Status]++
	}
	if statuses["planned"] < 50 || statuses["watching"] == 0 || statuses["finished"] == 0 {
		t.Errorf("statuses = %v", statuses)
	}
}

func TestSeedIsIdempotent(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	fetch := &fakeEnsurer{store: db}
	if _, err := seed(context.Background(), db, fetch, titles, now, false); err != nil {
		t.Fatal(err)
	}
	writes := db.writes
	first := db.items[0].AddedAt

	rep, err := seed(context.Background(), db, fetch, titles, now.Add(48*time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	if db.writes != writes {
		t.Errorf("the second run wrote %d times, want 0", db.writes-writes)
	}
	want := report{Titles: 76, ItemsExisting: 76, TitlesStored: 76}
	if rep != want {
		t.Errorf("second report = %+v, want %+v", rep, want)
	}
	if !db.items[0].AddedAt.Equal(first) {
		t.Error("the second run moved an existing item's date")
	}
	if !strings.Contains(rep.String(), "up to date") {
		t.Errorf("report text = %q", rep.String())
	}
}

func TestSeedAddsOnlyWhatIsMissing(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	fetch := &fakeEnsurer{store: db}
	if _, err := seed(context.Background(), db, fetch, titles, now, false); err != nil {
		t.Fatal(err)
	}

	// Someone changed the demo by hand: a status, a service, the name; one
	// item is gone and one that is not in the list was added.
	user := db.users[server.DemoUserEmail]
	user.Name = "Renamed"
	user.Services = user.Services[:3]
	user.Preferences.Digest.Enabled, user.Preferences.DigestConsent = true, true
	removed := db.items[len(db.items)-1]
	db.items = db.items[:len(db.items)-1]
	db.items[0].Status = models.WatchlistStatusFinished
	extra := &models.WatchlistItem{ID: primitive.NewObjectID(), UserID: user.ID, Title: "Extra", Type: "movie", TMDbID: 999999, Status: "planned", AddedAt: now}
	db.items = append(db.items, extra)

	rep, err := seed(context.Background(), db, fetch, titles, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.UserCreated || !rep.ProfileUpdated || !rep.ServicesSet || !rep.PreferencesReset || rep.ItemsInserted != 1 || rep.ItemsExisting != 75 {
		t.Errorf("report = %+v", rep)
	}
	if user.Name != "Demo" || len(user.ActiveServiceCodes()) != 6 || user.Preferences.Digest.Enabled || user.Preferences.DigestConsent {
		t.Errorf("user was not restored: %+v", user)
	}
	if db.items[0].Status != models.WatchlistStatusFinished {
		t.Error("an existing item's status was overwritten")
	}
	var restored, kept bool
	for _, item := range db.items {
		restored = restored || (item.TMDbID == removed.TMDbID && item.Type == removed.Type)
		kept = kept || item.ID == extra.ID
	}
	if !restored || !kept {
		t.Errorf("restored missing item = %v, kept extra item = %v", restored, kept)
	}
}

func TestSeedNeverTouchesAnotherUser(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	other := &models.User{
		ID: primitive.NewObjectID(), Email: "someone@example.com", Name: "Someone", Region: "CA",
		Services:    []models.ServiceSubscription{{Code: "tubi", Active: true}},
		Preferences: models.Preferences{DigestConsent: true, Digest: models.DigestSettings{Enabled: true}},
	}
	db.users[other.Email] = other
	theirs := &models.WatchlistItem{ID: primitive.NewObjectID(), UserID: other.ID, Title: "The Matrix", Type: "movie", TMDbID: 603, Status: "watching", AddedAt: now}
	db.items = append(db.items, theirs)
	before, beforeItem := *other, *theirs

	if _, err := seed(context.Background(), db, &fakeEnsurer{store: db}, titles, now, false); err != nil {
		t.Fatal(err)
	}
	if other.Name != before.Name || other.Region != before.Region || len(other.Services) != 1 || !other.Preferences.Digest.Enabled || !other.Preferences.DigestConsent {
		t.Errorf("another user was changed: %+v", other)
	}
	if !reflect.DeepEqual(*theirs, beforeItem) {
		t.Errorf("another user's item was changed: %+v", theirs)
	}
	owned := 0
	for _, item := range db.items {
		if item.UserID == other.ID {
			owned++
		}
	}
	if owned != 1 {
		t.Errorf("the other user now has %d items, want 1", owned)
	}
}

func TestSeedDryRunWritesNothing(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	// Two of the titles are already stored for someone else's list.
	db.titles["movie:603"] = &models.Title{ID: "movie:603", Providers: map[string]models.RegionSnapshot{"US": {FetchedAt: now}}}
	db.titles["tv:1396"] = &models.Title{ID: "tv:1396", Providers: map[string]models.RegionSnapshot{"US": {FetchedAt: now}}}

	// A nil ensurer would panic if a dry run tried to fetch.
	rep, err := seed(context.Background(), db, nil, titles, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if db.writes != 0 || len(db.users) != 0 || len(db.items) != 0 {
		t.Errorf("the dry run wrote: %d writes, %d users, %d items", db.writes, len(db.users), len(db.items))
	}
	want := report{DryRun: true, Titles: 76, UserCreated: true, ServicesSet: true, PreferencesReset: true, ItemsInserted: 76, TitlesStored: 2, TitlesFetched: 74}
	if rep != want {
		t.Errorf("report = %+v, want %+v", rep, want)
	}
	text := rep.String()
	for _, part := range []string{"nothing was written", "would be created", "76 to insert", "2 already stored, 74 to fetch"} {
		if !strings.Contains(text, part) {
			t.Errorf("report text lacks %q:\n%s", part, text)
		}
	}
	if strings.Contains(text, "@") {
		t.Errorf("the report contains an address:\n%s", text)
	}
}

func TestSeedReportsTitlesItCouldNotFetch(t *testing.T) {
	titles := mustTitles(t)
	db := newFakeStore()
	fetch := &fakeEnsurer{store: db, fail: map[string]bool{"movie:603": true, "tv:1399": true}}

	rep, err := seed(context.Background(), db, fetch, titles, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TitlesFetched != 74 || rep.TitlesUnresolved != 2 || rep.ItemsInserted != 76 {
		t.Errorf("report = %+v", rep)
	}
	if !strings.Contains(rep.String(), "2 could not be fetched") {
		t.Errorf("report text = %q", rep.String())
	}

	// The next run fetches only what is still missing.
	fetch.fail = nil
	rep, err = seed(context.Background(), db, fetch, titles, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if rep.TitlesStored != 74 || rep.TitlesFetched != 2 || rep.TitlesUnresolved != 0 || rep.ItemsInserted != 0 {
		t.Errorf("retry report = %+v", rep)
	}
}

func TestAddedAtIsStableAndMixed(t *testing.T) {
	titles := mustTitles(t)
	a, b := addedAt(titles, now), addedAt(titles, now)
	if len(a) != len(titles) {
		t.Fatalf("%d times for %d titles", len(a), len(titles))
	}
	for key, at := range a {
		if !b[key].Equal(at) {
			t.Fatalf("%s: times differ between runs", key)
		}
	}
	// The newest dozen should not all be the same kind.
	type dated struct {
		typ string
		at  time.Time
	}
	var newest []dated
	for _, title := range titles {
		newest = append(newest, dated{title.Type, a[title.ref().Key()]})
	}
	for i := range newest {
		for j := i + 1; j < len(newest); j++ {
			if newest[j].at.After(newest[i].at) {
				newest[i], newest[j] = newest[j], newest[i]
			}
		}
	}
	kinds := map[string]int{}
	for _, d := range newest[:12] {
		kinds[d.typ]++
	}
	if kinds["movie"] == 0 || kinds["show"] == 0 {
		t.Errorf("the 12 newest are %v, want films and shows mixed", kinds)
	}
}
