package database

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// These tests run against the MongoDB container TestMain starts. They pin the
// behaviour the availability store and the digest claim rely on MongoDB for:
// conditional updates, the duplicate-key path of a conditional upsert,
// de-duplicating inserts and region projections. Each test uses its own keys,
// so they do not depend on order.

var t1 = time.Date(2026, 3, 1, 6, 0, 0, 0, time.UTC)

func snapshot(at time.Time, providers ...string) models.RegionSnapshot {
	snap := models.RegionSnapshot{Link: "https://example.test/watch", Offers: []models.ProviderOffer{}, FetchedAt: at}
	for i, name := range providers {
		snap.Offers = append(snap.Offers, models.ProviderOffer{
			ProviderID: 8 + i, ProviderName: name, LogoPath: "/logo.jpg", Access: models.AccessSubscription,
		})
	}
	return snap
}

func mustGetTitle(t *testing.T, srv Service, key string, regions ...string) *models.Title {
	t.Helper()
	titles, err := srv.GetTitles(context.Background(), []string{key, "movie:0"}, regions)
	if err != nil {
		t.Fatalf("GetTitles: %v", err)
	}
	return titles[key]
}

func TestSaveTitleSnapshotIsACompareAndSet(t *testing.T) {
	srv := New()
	ctx := context.Background()
	ref := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 900001}
	metaAt := t1
	meta := &models.TitleMetadata{Title: "The Matrix", Year: 1999, PosterPath: "/m.jpg", Genres: []string{"Action"}, Rating: 8.2, MetadataFetchedAt: &metaAt}
	first := snapshot(t1, "Netflix")
	second := snapshot(t1.Add(24*time.Hour), "Netflix", "Hulu")

	if got := mustGetTitle(t, srv, ref.Key()); got != nil {
		t.Fatalf("title exists before the test: %+v", got)
	}

	// A baseline creates the document.
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", nil, first, meta); err != nil || !ok {
		t.Fatalf("baseline: applied = %v, err = %v", ok, err)
	}
	stored := mustGetTitle(t, srv, ref.Key())
	if stored == nil {
		t.Fatal("baseline was not stored")
	}
	snap, ok := stored.Snapshot("US")
	if !ok || !snap.FetchedAt.Equal(t1) || !reflect.DeepEqual(snap.Offers, first.Offers) || snap.Link != first.Link {
		t.Errorf("stored snapshot = %+v (present: %v), want %+v", snap, ok, first)
	}
	if stored.MediaType != "movie" || stored.TMDbID != 900001 || stored.Title != "The Matrix" || stored.Rating != 8.2 || stored.CreatedAt.IsZero() {
		t.Errorf("stored title = %+v", stored)
	}

	// A second baseline loses: the title already has a snapshot for the region.
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", nil, second, nil); err != nil || ok {
		t.Errorf("second baseline: applied = %v, err = %v; want not applied and no error", ok, err)
	}
	// So does a replace that read a version which is no longer stored.
	stale := t1.Add(-time.Hour)
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", &stale, second, nil); err != nil || ok {
		t.Errorf("stale replace: applied = %v, err = %v; want not applied and no error", ok, err)
	}
	if snap, _ := mustGetTitle(t, srv, ref.Key()).Snapshot("US"); !snap.FetchedAt.Equal(t1) || len(snap.Offers) != 1 {
		t.Errorf("a lost write changed the snapshot: %+v", snap)
	}

	// A replace that read the current version wins, and keeps the metadata
	// when none is passed.
	prev := snap.FetchedAt
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", &prev, second, nil); err != nil || !ok {
		t.Fatalf("replace: applied = %v, err = %v", ok, err)
	}
	stored = mustGetTitle(t, srv, ref.Key())
	if snap, _ := stored.Snapshot("US"); !snap.FetchedAt.Equal(second.FetchedAt) || len(snap.Offers) != 2 {
		t.Errorf("replaced snapshot = %+v", snap)
	}
	if stored.Title != "The Matrix" || len(stored.Genres) != 1 {
		t.Errorf("metadata was lost by a snapshot-only write: %+v", stored.TitleMetadata)
	}

	// Another region gets its own baseline on the same document.
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "GB", nil, snapshot(t1, "Disney Plus"), nil); err != nil || !ok {
		t.Fatalf("baseline for a second region: applied = %v, err = %v", ok, err)
	}
	if got := mustGetTitle(t, srv, ref.Key()); len(got.Providers) != 2 {
		t.Errorf("regions stored = %d, want 2: %+v", len(got.Providers), got.Providers)
	}

	// A region projection returns that region only, with the metadata.
	gb := mustGetTitle(t, srv, ref.Key(), "GB")
	if _, ok := gb.Snapshot("US"); ok || len(gb.Providers) != 1 {
		t.Errorf("projection to GB returned regions %v", gb.Providers)
	}
	if gb.Title != "The Matrix" || gb.PosterPath != "/m.jpg" || gb.TMDbID != 900001 {
		t.Errorf("projection dropped metadata: %+v", gb)
	}
	if none := mustGetTitle(t, srv, ref.Key(), "FR"); none == nil || len(none.Providers) != 0 {
		t.Errorf("projection to a region without a snapshot = %+v, want the title with no providers", none)
	}

	if _, err := srv.SaveTitleSnapshot(ctx, ref, "not-a-region", nil, first, nil); err == nil {
		t.Error("an invalid region was accepted")
	}
}

func TestMarkTitleNotFound(t *testing.T) {
	srv := New()
	ctx := context.Background()
	ref := models.TitleRef{MediaType: models.MediaTypeTV, TMDbID: 900002}

	// Unknown from the start: a tombstone without snapshots.
	if err := srv.MarkTitleNotFound(ctx, ref, t1); err != nil {
		t.Fatal(err)
	}
	tomb := mustGetTitle(t, srv, ref.Key(), "US")
	if tomb == nil || tomb.NotFoundAt == nil || !tomb.NotFoundAt.Equal(t1) || len(tomb.Providers) != 0 {
		t.Fatalf("tombstone = %+v", tomb)
	}

	// TMDb has it after all: the baseline lands on the tombstone and clears it.
	if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", nil, snapshot(t1.Add(time.Hour), "Netflix"), nil); err != nil || !ok {
		t.Fatalf("baseline over a tombstone: applied = %v, err = %v", ok, err)
	}
	if got := mustGetTitle(t, srv, ref.Key(), "US"); got.NotFoundAt != nil {
		t.Errorf("not_found_at survived a successful fetch: %v", got.NotFoundAt)
	}

	// Gone again: the last snapshot is kept.
	if err := srv.MarkTitleNotFound(ctx, ref, t1.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got := mustGetTitle(t, srv, ref.Key(), "US")
	if got.NotFoundAt == nil {
		t.Error("not_found_at was not set")
	}
	if snap, ok := got.Snapshot("US"); !ok || len(snap.Offers) != 1 {
		t.Errorf("marking a title not found dropped its snapshot: %+v", got.Providers)
	}
}

func TestAvailabilityEventsAreDeduplicated(t *testing.T) {
	srv := New()
	ctx := context.Background()
	event := func(id, titleKey, region string, at time.Time) models.AvailabilityEvent {
		return models.AvailabilityEvent{
			ID: id, TitleKey: titleKey, Region: region, ServiceCode: models.ServiceNetflix, ProviderName: "Netflix",
			Access: models.AccessSubscription, Change: models.ChangeAdded, DetectedAt: at, ExpiresAt: at.Add(models.EventRetention),
		}
	}
	// Recent, so the TTL monitor leaves them alone while the test runs.
	now := time.Now().UTC().Truncate(time.Millisecond)
	a := event("it-dedupe-a", "movie:900010", "US", now.Add(-3*time.Hour))
	b := event("it-dedupe-b", "movie:900011", "US", now.Add(-2*time.Hour))
	c := event("it-dedupe-c", "movie:900010", "US", now.Add(-1*time.Hour))
	elsewhere := event("it-dedupe-d", "movie:900010", "GB", now.Add(-1*time.Hour))

	if n, err := srv.InsertAvailabilityEvents(ctx, nil); err != nil || n != 0 {
		t.Errorf("inserting nothing: n = %d, err = %v", n, err)
	}
	if n, err := srv.InsertAvailabilityEvents(ctx, []models.AvailabilityEvent{a, b}); err != nil || n != 2 {
		t.Fatalf("first insert: n = %d, err = %v; want 2", n, err)
	}
	// A retry that overlaps: the known events are skipped, the new ones land
	// even though they come after a duplicate in the batch.
	if n, err := srv.InsertAvailabilityEvents(ctx, []models.AvailabilityEvent{a, b, c, elsewhere}); err != nil || n != 2 {
		t.Fatalf("overlapping insert: n = %d, err = %v; want 2 new", n, err)
	}
	if n, err := srv.InsertAvailabilityEvents(ctx, []models.AvailabilityEvent{a, b, c}); err != nil || n != 0 {
		t.Errorf("repeated insert: n = %d, err = %v; want 0 new", n, err)
	}

	ids := func(events []models.AvailabilityEvent) []string {
		out := []string{}
		for _, ev := range events {
			out = append(out, ev.ID)
		}
		return out
	}
	keys := []string{"movie:900010", "movie:900011"}

	all, err := srv.ListAvailabilityEvents(ctx, keys, "US", now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"it-dedupe-a", "it-dedupe-b", "it-dedupe-c"}; !reflect.DeepEqual(ids(all), want) {
		t.Errorf("US events = %v, want %v (oldest first, other regions left out)", ids(all), want)
	}
	if len(all) > 0 && (!all[0].DetectedAt.Equal(a.DetectedAt) || all[0].ServiceCode != models.ServiceNetflix || all[0].Change != models.ChangeAdded) {
		t.Errorf("event did not round-trip: %+v", all[0])
	}

	since, err := srv.ListAvailabilityEvents(ctx, keys, "US", b.DetectedAt)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"it-dedupe-b", "it-dedupe-c"}; !reflect.DeepEqual(ids(since), want) {
		t.Errorf("events since b = %v, want %v (inclusive)", ids(since), want)
	}

	one, err := srv.ListAvailabilityEvents(ctx, []string{"movie:900011"}, "US", now.Add(-24*time.Hour))
	if err != nil || !reflect.DeepEqual(ids(one), []string{"it-dedupe-b"}) {
		t.Errorf("events for one title = %v, err = %v", ids(one), err)
	}
	if none, err := srv.ListAvailabilityEvents(ctx, nil, "US", now.Add(-24*time.Hour)); err != nil || len(none) != 0 {
		t.Errorf("events for no titles = %v, err = %v", ids(none), err)
	}
}

func TestDigestDeliveries(t *testing.T) {
	srv := New()
	ctx := context.Background()
	alice, bob := primitive.NewObjectID(), primitive.NewObjectID()
	// Recent, so the TTL monitor leaves them alone while the test runs.
	sentAt := time.Now().UTC().Truncate(time.Millisecond)
	delivery := func(user primitive.ObjectID, eventID string) models.DigestDelivery {
		return models.DigestDelivery{UserID: user, EventID: eventID, SentAt: sentAt, ExpiresAt: sentAt.Add(models.DeliveryRetention)}
	}
	delivered := func(user primitive.ObjectID, eventIDs ...string) map[string]bool {
		t.Helper()
		got, err := srv.ListDeliveredEventIDs(ctx, user, eventIDs)
		if err != nil {
			t.Fatalf("ListDeliveredEventIDs: %v", err)
		}
		return got
	}

	if n, err := srv.InsertDigestDeliveries(ctx, nil); err != nil || n != 0 {
		t.Errorf("inserting nothing: n = %d, err = %v", n, err)
	}
	if got := delivered(alice, "it-ev-a", "it-ev-b"); len(got) != 0 {
		t.Errorf("delivered before anything was recorded = %v", got)
	}

	if n, err := srv.InsertDigestDeliveries(ctx, []models.DigestDelivery{delivery(alice, "it-ev-a"), delivery(alice, "it-ev-b")}); err != nil || n != 2 {
		t.Fatalf("first insert: n = %d, err = %v; want 2", n, err)
	}
	// One record per user and event: a repeat is skipped, and the new records
	// around it still land, including the same event for another user.
	batch := []models.DigestDelivery{delivery(alice, "it-ev-a"), delivery(alice, "it-ev-c"), delivery(bob, "it-ev-a")}
	if n, err := srv.InsertDigestDeliveries(ctx, batch); err != nil || n != 2 {
		t.Fatalf("overlapping insert: n = %d, err = %v; want 2 new", n, err)
	}

	if got, want := delivered(alice, "it-ev-a", "it-ev-b", "it-ev-c", "it-ev-never"), (map[string]bool{"it-ev-a": true, "it-ev-b": true, "it-ev-c": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("alice's deliveries = %v, want %v", got, want)
	}
	if got, want := delivered(alice, "it-ev-b"), (map[string]bool{"it-ev-b": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("deliveries are not limited to the events asked about: %v, want %v", got, want)
	}
	if got, want := delivered(bob, "it-ev-a", "it-ev-b"), (map[string]bool{"it-ev-a": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("bob's deliveries = %v, want %v", got, want)
	}
	if got := delivered(alice); len(got) != 0 {
		t.Errorf("deliveries for no events = %v", got)
	}

	// Undoing a failed send removes that user's records for those events only.
	if n, err := srv.DeleteDigestDeliveries(ctx, alice, []string{"it-ev-a", "it-ev-c", "it-ev-never"}); err != nil || n != 2 {
		t.Fatalf("delete: n = %d, err = %v; want 2", n, err)
	}
	if n, err := srv.DeleteDigestDeliveries(ctx, alice, nil); err != nil || n != 0 {
		t.Errorf("deleting nothing: n = %d, err = %v", n, err)
	}
	if got, want := delivered(alice, "it-ev-a", "it-ev-b", "it-ev-c"), (map[string]bool{"it-ev-b": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("alice's deliveries after the delete = %v, want %v", got, want)
	}
	if got, want := delivered(bob, "it-ev-a"), (map[string]bool{"it-ev-a": true}); !reflect.DeepEqual(got, want) {
		t.Errorf("deleting alice's records touched bob's: %v", got)
	}
	// Removed records can be written again, as the retry after a failed send does.
	if n, err := srv.InsertDigestDeliveries(ctx, []models.DigestDelivery{delivery(alice, "it-ev-a")}); err != nil || n != 1 {
		t.Errorf("re-insert after delete: n = %d, err = %v; want 1", n, err)
	}
}

// Both collections that grow without bound expire through a TTL index on an
// absolute expires_at.
func TestExpiringCollectionsHaveATTLIndex(t *testing.T) {
	srv := New().(*service)
	ctx := context.Background()
	for _, name := range []string{collEvents, collDelivered} {
		cursor, err := srv.db.Database(databaseName).Collection(name).Indexes().List(ctx)
		if err != nil {
			t.Fatalf("%s: list indexes: %v", name, err)
		}
		var indexes []bson.M
		if err := cursor.All(ctx, &indexes); err != nil {
			t.Fatalf("%s: read indexes: %v", name, err)
		}
		found := false
		for _, index := range indexes {
			if index["name"] != "expires_at_ttl" {
				continue
			}
			found = true
			// The numeric type MongoDB reports is not what matters here.
			if got := fmt.Sprint(index["expireAfterSeconds"]); got != "0" {
				t.Errorf("%s: expireAfterSeconds = %s, want 0", name, got)
			}
		}
		if !found {
			t.Errorf("%s has no expires_at_ttl index: %v", name, indexes)
		}
	}
}

func TestDigestScheduleClaim(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const email = "it-claim@example.com"
	if err := srv.UpsertUser(ctx, &models.User{Email: email, Name: "Claim"}); err != nil {
		t.Fatal(err)
	}
	user, err := srv.GetUserByEmail(ctx, email)
	if err != nil || user == nil {
		t.Fatalf("GetUserByEmail: %v, %v", user, err)
	}

	isDue := func(at time.Time) bool {
		t.Helper()
		due, err := srv.GetUsersForDigest(ctx, at)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range due {
			if u.ID == user.ID {
				return true
			}
		}
		return false
	}
	schedule := func() models.DigestSchedule {
		t.Helper()
		u, err := srv.GetUserByEmail(ctx, email)
		if err != nil || u == nil {
			t.Fatalf("GetUserByEmail: %v, %v", u, err)
		}
		return u.Preferences.Digest.Schedule()
	}

	sentAt := t1
	next := t1.Add(7 * 24 * time.Hour)
	never := models.DigestSchedule{}
	claimed := models.DigestSchedule{LastSentAt: &sentAt, NextScheduledAt: &next}

	// A new user has digests off: not due, and cannot be claimed.
	if isDue(t1) {
		t.Error("a user with digests off is due")
	}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, never, claimed, true); err != nil || won {
		t.Errorf("claim with digests off: won = %v, err = %v", won, err)
	}

	if _, err := srv.UpdateUserPreferences(ctx, user.ID, map[string]any{
		"preferences.digest_consent": true,
		"preferences.digest.enabled": true,
	}); err != nil {
		t.Fatal(err)
	}
	// Never scheduled counts as due: the schedule fields are absent.
	if !isDue(t1) {
		t.Error("an opted-in user who was never scheduled is not due")
	}

	// A claim made from the wrong idea of the schedule loses.
	wrong := models.DigestSchedule{LastSentAt: &next}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, wrong, claimed, true); err != nil || won {
		t.Errorf("claim from a stale read: won = %v, err = %v", won, err)
	}

	// The first claim wins; an identical second one, as an overlapping run
	// would make, loses.
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, never, claimed, true); err != nil || !won {
		t.Fatalf("first claim: won = %v, err = %v", won, err)
	}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, never, claimed, true); err != nil || won {
		t.Errorf("second claim: won = %v, err = %v; want lost", won, err)
	}
	got := schedule()
	if got.LastSentAt == nil || !got.LastSentAt.Equal(sentAt) || got.NextScheduledAt == nil || !got.NextScheduledAt.Equal(next) {
		t.Errorf("schedule after the claim = %+v", got)
	}
	if isDue(next.Add(-time.Second)) {
		t.Error("a claimed user is due before their next send")
	}
	if !isDue(next) {
		t.Error("a claimed user is not due at their next send")
	}

	// Releasing the claim puts back exactly what was there: no schedule.
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, claimed, never, false); err != nil || !won {
		t.Fatalf("release: won = %v, err = %v", won, err)
	}
	if got := schedule(); got.LastSentAt != nil || got.NextScheduledAt != nil {
		t.Errorf("schedule after the release = %+v, want empty", got)
	}
	if !isDue(t1) {
		t.Error("a released user is not due again")
	}

	// Claim from a schedule that has values, as every later digest does.
	later := models.DigestSchedule{LastSentAt: &next, NextScheduledAt: &next}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, never, claimed, true); err != nil || !won {
		t.Fatalf("claim again: won = %v, err = %v", won, err)
	}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, claimed, later, true); err != nil || !won {
		t.Errorf("claim from a set schedule: won = %v, err = %v", won, err)
	}

	// Opting out between the read and the claim stops the send.
	if _, err := srv.UpdateUserPreferences(ctx, user.ID, map[string]any{"preferences.digest.enabled": false}); err != nil {
		t.Fatal(err)
	}
	if won, err := srv.CompareAndSwapDigestSchedule(ctx, user.ID, later, claimed, true); err != nil || won {
		t.Errorf("claim after opting out: won = %v, err = %v; want lost", won, err)
	}
}

func TestDistinctTitlesAndRegions(t *testing.T) {
	srv := New()
	ctx := context.Background()

	var ids [2]*models.User
	for i, u := range []models.User{{Email: "it-distinct-us@example.com", Region: "US"}, {Email: "it-distinct-gb@example.com", Region: "GB"}} {
		if err := srv.UpsertUser(ctx, &u); err != nil {
			t.Fatal(err)
		}
		stored, err := srv.GetUserByEmail(ctx, u.Email)
		if err != nil || stored == nil {
			t.Fatalf("GetUserByEmail: %v, %v", stored, err)
		}
		ids[i] = stored
	}
	add := func(user *models.User, typ string, tmdbID int) {
		t.Helper()
		item := &models.WatchlistItem{UserID: user.ID, Title: "Title", Type: typ, TMDbID: tmdbID}
		if err := srv.CreateWatchlistItem(ctx, item); err != nil {
			t.Fatalf("CreateWatchlistItem: %v", err)
		}
	}
	add(ids[0], models.WatchlistTypeMovie, 900020)
	add(ids[1], models.WatchlistTypeShow, 900020) // a show with the same number is another title
	add(ids[0], models.WatchlistTypeMovie, 900023)
	add(ids[1], models.WatchlistTypeMovie, 900023) // the same title on two lists
	add(ids[1], models.WatchlistTypeShow, 900021)

	refs, err := srv.DistinctWatchlistTitles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, ref := range refs {
		count[ref.Key()]++
	}
	for _, key := range []string{"movie:900020", "tv:900020", "movie:900023", "tv:900021"} {
		if count[key] != 1 {
			t.Errorf("%s appears %d times in %v, want once", key, count[key], refs)
		}
	}

	regions, err := srv.DistinctUserRegions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, region := range regions {
		has[region] = true
	}
	if !has["US"] || !has["GB"] {
		t.Errorf("regions = %v, want US and GB among them", regions)
	}

	// The list the watchlist view reads: one user's items only.
	items, err := srv.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: ids[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Errorf("user has %d items, want 3", len(items))
	}

	// Upserting a user again, with a region, updates rather than conflicts.
	if err := srv.UpsertUser(ctx, &models.User{Email: "it-distinct-gb@example.com", Region: "GB", Name: "Renamed"}); err != nil {
		t.Errorf("second upsert: %v", err)
	}
}

// --- IMDb sync: indexes, bulk insert, conditional writes, the link ---

func indexNames(t *testing.T, coll *mongo.Collection) map[string]bool {
	t.Helper()
	ctx := context.Background()
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		t.Fatalf("%s: list indexes: %v", coll.Name(), err)
	}
	var indexes []bson.M
	if err := cursor.All(ctx, &indexes); err != nil {
		t.Fatalf("%s: read indexes: %v", coll.Name(), err)
	}
	names := make(map[string]bool, len(indexes))
	for _, index := range indexes {
		names[fmt.Sprint(index["name"])] = true
	}
	return names
}

func mustFindItem(t *testing.T, srv Service, userID, itemID primitive.ObjectID) *models.WatchlistItem {
	t.Helper()
	items, err := srv.ListWatchlistItems(context.Background(), models.ListWatchlistOptions{UserID: userID})
	if err != nil {
		t.Fatalf("ListWatchlistItems: %v", err)
	}
	for _, item := range items {
		if item.ID == itemID {
			return item
		}
	}
	return nil
}

// TMDb IDs are per media type, so the unique index has to include the type.
func TestWatchlistUniqueIndexes(t *testing.T) {
	srv := New().(*service)
	ctx := context.Background()

	watchlist := indexNames(t, srv.db.Database(databaseName).Collection(collWatchlist))
	if !watchlist[indexUserTypeTMDb] || !watchlist["uniq_user_imdb_id"] || watchlist[legacyIndexUserTMDb] {
		t.Errorf("watchlist indexes = %v; want %s and uniq_user_imdb_id, and no %s", watchlist, indexUserTypeTMDb, legacyIndexUserTMDb)
	}
	if titles := indexNames(t, srv.db.Database(databaseName).Collection(collTitles)); !titles["imdb_id_idx"] {
		t.Errorf("titles indexes = %v; want imdb_id_idx", titles)
	}

	alice, bob := primitive.NewObjectID(), primitive.NewObjectID()
	create := func(user primitive.ObjectID, typ string, tmdbID int, imdbID string) error {
		return srv.CreateWatchlistItem(ctx, &models.WatchlistItem{UserID: user, Title: "Title", Type: typ, TMDbID: tmdbID, IMDbID: imdbID})
	}
	if err := create(alice, models.WatchlistTypeMovie, 900100, "tt9001000"); err != nil {
		t.Fatalf("first item: %v", err)
	}
	if err := create(alice, models.WatchlistTypeShow, 900100, "tt9001001"); err != nil {
		t.Errorf("a show with the same TMDb ID as a movie was refused: %v", err)
	}
	if err := create(alice, models.WatchlistTypeMovie, 900100, "tt9001002"); !errors.Is(err, ErrDuplicateWatchlistItem) {
		t.Errorf("the same movie twice: err = %v, want ErrDuplicateWatchlistItem", err)
	}
	if err := create(alice, models.WatchlistTypeMovie, 900101, "tt9001000"); !errors.Is(err, ErrDuplicateWatchlistItem) {
		t.Errorf("the same IMDb ID twice: err = %v, want ErrDuplicateWatchlistItem", err)
	}
	if err := create(bob, models.WatchlistTypeMovie, 900100, "tt9001000"); err != nil {
		t.Errorf("another user's copy of the title was refused: %v", err)
	}
	// Items without either ID never collide.
	for i := 0; i < 2; i++ {
		if err := create(alice, models.WatchlistTypeMovie, 0, ""); err != nil {
			t.Errorf("item without IDs, copy %d: %v", i+1, err)
		}
	}
}

// A database that still has the old index gets the new one first and loses
// the old one after. This runs in a database of its own, because it needs
// the old index to exist.
func TestLegacyTMDbIndexIsReplaced(t *testing.T) {
	srv := New().(*service)
	ctx := context.Background()
	db := srv.db.Database(databaseName + "_legacy_index")
	t.Cleanup(func() { _ = db.Drop(context.Background()) })
	coll := db.Collection(collWatchlist)

	legacy := mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "tmdb_id", Value: 1}},
		Options: options.Index().SetUnique(true).
			SetPartialFilterExpression(bson.D{{Key: "tmdb_id", Value: bson.D{{Key: "$exists", Value: true}}}}).
			SetName(legacyIndexUserTMDb),
	}
	if _, err := coll.Indexes().CreateOne(ctx, legacy); err != nil {
		t.Fatalf("create the legacy index: %v", err)
	}
	user := primitive.NewObjectID()
	doc := func(typ string) bson.M {
		return bson.M{"user_id": user, "title": "Title", "type": typ, "tmdb_id": 603, "status": "planned"}
	}
	if _, err := coll.InsertOne(ctx, doc(models.WatchlistTypeMovie)); err != nil {
		t.Fatalf("insert under the legacy index: %v", err)
	}
	if _, err := coll.InsertOne(ctx, doc(models.WatchlistTypeShow)); !mongo.IsDuplicateKeyError(err) {
		t.Fatalf("the legacy index should refuse a show with a movie's TMDb ID; err = %v", err)
	}

	// Starting the new build twice is the same as starting it once.
	for start := 1; start <= 2; start++ {
		if err := ensureIndexes(ctx, db); err != nil {
			t.Fatalf("ensureIndexes, start %d: %v", start, err)
		}
		if names := indexNames(t, coll); !names[indexUserTypeTMDb] || names[legacyIndexUserTMDb] {
			t.Fatalf("after start %d the indexes are %v; want %s without %s", start, names, indexUserTypeTMDb, legacyIndexUserTMDb)
		}
	}

	if _, err := coll.InsertOne(ctx, doc(models.WatchlistTypeShow)); err != nil {
		t.Errorf("a show with a movie's TMDb ID is still refused: %v", err)
	}
	if _, err := coll.InsertOne(ctx, doc(models.WatchlistTypeMovie)); !mongo.IsDuplicateKeyError(err) {
		t.Errorf("the same movie twice: err = %v, want a duplicate-key error", err)
	}

	// What a rollback meets: the previous build creates the legacy index at
	// startup, and that build fails once a user holds a movie and a show with
	// the same TMDb ID.
	if _, err := coll.Indexes().CreateOne(ctx, legacy); err == nil {
		t.Error("the legacy index was rebuilt over data it does not allow")
	}
}

func TestCreateWatchlistItems(t *testing.T) {
	srv := New()
	ctx := context.Background()
	user := primitive.NewObjectID()
	item := func(title, typ string, tmdbID int, imdbID string) *models.WatchlistItem {
		return &models.WatchlistItem{UserID: user, Title: title, Type: typ, TMDbID: tmdbID, IMDbID: imdbID, Source: models.SourceIMDb}
	}

	if created, err := srv.CreateWatchlistItems(ctx, nil); err != nil || len(created) != 0 {
		t.Errorf("inserting nothing: created = %v, err = %v", created, err)
	}
	if err := srv.CreateWatchlistItem(ctx, &models.WatchlistItem{UserID: user, Title: "Already There", Type: models.WatchlistTypeMovie, TMDbID: 900200, IMDbID: "tt9002000"}); err != nil {
		t.Fatal(err)
	}

	batch := func() []*models.WatchlistItem {
		return []*models.WatchlistItem{
			item("First", models.WatchlistTypeMovie, 900201, "tt9002001"),
			item("Same TMDb title", models.WatchlistTypeMovie, 900200, "tt9002009"),
			item("Same IMDb ID", models.WatchlistTypeShow, 900202, "tt9002000"),
			item("Last", models.WatchlistTypeShow, 900200, "tt9002003"), // a show numbered like the movie
		}
	}
	created, err := srv.CreateWatchlistItems(ctx, batch())
	if err != nil {
		t.Fatalf("CreateWatchlistItems: %v", err)
	}
	if len(created) != 2 || created[0].Title != "First" || created[1].Title != "Last" {
		t.Fatalf("created = %+v; want First and Last, with the two duplicates skipped", created)
	}
	for _, c := range created {
		if c.ID.IsZero() || c.Status != models.WatchlistStatusPlanned || c.AddedAt.IsZero() || !c.UpdatedAt.Equal(c.AddedAt) {
			t.Errorf("created item not filled in: %+v", c)
		}
	}
	if !created[0].AddedAt.Before(created[1].AddedAt) {
		t.Errorf("items are not stamped in slice order: %v then %v", created[0].AddedAt, created[1].AddedAt)
	}

	stored, err := srv.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("user has %d items, want 3", len(stored))
	}
	position := map[string]int{}
	for i, it := range stored {
		position[it.Title] = i
	}
	if position["Last"] > position["First"] {
		t.Errorf("newest-first listing shows First before Last; want the end of the batch first: %v", position)
	}
	if first := mustFindItem(t, srv, user, created[0].ID); first == nil || first.Source != models.SourceIMDb || first.IMDbID != "tt9002001" || first.IMDbMissingSince != nil {
		t.Errorf("stored item = %+v", first)
	}

	// The same batch again creates nothing and is not an error.
	if again, err := srv.CreateWatchlistItems(ctx, batch()); err != nil || len(again) != 0 {
		t.Errorf("second insert of the batch: created = %d, err = %v; want 0 and nil", len(again), err)
	}
	// An invalid item fails the call before anything is written.
	bad := []*models.WatchlistItem{item("Fine", models.WatchlistTypeMovie, 900203, "tt9002004"), item("", models.WatchlistTypeMovie, 900204, "tt9002005")}
	if created, err := srv.CreateWatchlistItems(ctx, bad); err == nil || len(created) != 0 {
		t.Errorf("batch with an invalid item: created = %d, err = %v", len(created), err)
	}
	if after, _ := srv.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user}); len(after) != 3 {
		t.Errorf("user has %d items after the rejected batches, want 3", len(after))
	}
}

// The sync's writes carry their own conditions, so a user's edit that lands
// between planning and writing wins.
func TestIMDbSyncItemWrites(t *testing.T) {
	srv := New()
	ctx := context.Background()
	user, stranger := primitive.NewObjectID(), primitive.NewObjectID()

	created, err := srv.CreateWatchlistItems(ctx, []*models.WatchlistItem{
		{UserID: user, Title: "Synced", Type: models.WatchlistTypeMovie, TMDbID: 900300, IMDbID: "tt9003000", Source: models.SourceIMDb},
		{UserID: user, Title: "Synced Too", Type: models.WatchlistTypeMovie, TMDbID: 900303, IMDbID: "tt9003003", Source: models.SourceIMDb},
		{UserID: user, Title: "Manual", Type: models.WatchlistTypeMovie, TMDbID: 900301},
		{UserID: user, Title: "Manual Too", Type: models.WatchlistTypeMovie, TMDbID: 900302},
	})
	if err != nil || len(created) != 4 {
		t.Fatalf("seed: created = %d, err = %v", len(created), err)
	}
	synced, syncedToo, manual, manualToo := created[0], created[1], created[2], created[3]
	check := func(what string, got bool, err error, want bool) {
		t.Helper()
		if err != nil || got != want {
			t.Errorf("%s: applied = %v, err = %v; want %v and no error", what, got, err, want)
		}
	}
	edit := func(itemID primitive.ObjectID, fields map[string]any) {
		t.Helper()
		if _, err := srv.UpdateWatchlistItem(ctx, user, itemID, fields); err != nil {
			t.Fatalf("UpdateWatchlistItem: %v", err)
		}
	}

	// seen is an item's updated_at as a plan reading it now would see it.
	seen := func(itemID primitive.ObjectID) time.Time {
		t.Helper()
		return mustFindItem(t, srv, user, itemID).UpdatedAt
	}
	// The sync's own writes must not move updated_at: the delete relies on
	// that field changing only when the user edits the item.
	untouched := func(what string, itemID primitive.ObjectID, before time.Time) {
		t.Helper()
		if got := seen(itemID); !got.Equal(before) {
			t.Errorf("%s moved updated_at from %v to %v", what, before, got)
		}
	}

	// Filling in an IMDb ID.
	manualSeen := seen(manual.ID)
	ok, err := srv.SetWatchlistItemIMDbID(ctx, stranger, manual.ID, "tt9003001")
	check("set IMDb ID on someone else's item", ok, err, false)
	ok, err = srv.SetWatchlistItemIMDbID(ctx, user, manual.ID, "tt9003001")
	check("set a missing IMDb ID", ok, err, true)
	ok, err = srv.SetWatchlistItemIMDbID(ctx, user, manual.ID, "tt9003009")
	check("overwrite an IMDb ID", ok, err, false)
	ok, err = srv.SetWatchlistItemIMDbID(ctx, user, manualToo.ID, "tt9003000")
	check("set an IMDb ID another item holds", ok, err, false)
	if got := mustFindItem(t, srv, user, manual.ID); got.IMDbID != "tt9003001" || got.Source != "" {
		t.Errorf("manual item = %+v; want the IMDb ID filled in and the source untouched", got)
	}
	untouched("filling in an IMDb ID", manual.ID, manualSeen)
	if got := mustFindItem(t, srv, user, manualToo.ID); got.IMDbID != "" {
		t.Errorf("refused IMDb ID was written anyway: %+v", got)
	}

	// Strike one.
	strike := t1
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, manual.ID, strike)
	check("mark an item the sync did not add", ok, err, false)
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, stranger, synced.ID, strike)
	check("mark someone else's item", ok, err, false)
	// An item the user has made theirs takes no strike, like it takes no delete.
	edit(synced.ID, map[string]any{"status": models.WatchlistStatusWatching})
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, synced.ID, strike)
	check("mark after the user changed the status", ok, err, false)
	edit(synced.ID, map[string]any{"status": models.WatchlistStatusPlanned, "tags": []string{"keep"}})
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, synced.ID, strike)
	check("mark after the user tagged it", ok, err, false)
	if got := mustFindItem(t, srv, user, synced.ID); got.IMDbMissingSince != nil {
		t.Fatalf("a refused mark was written: %+v", got)
	}
	edit(synced.ID, map[string]any{"tags": []string{}})
	syncedSeen := seen(synced.ID)
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, synced.ID, strike)
	check("mark", ok, err, true)
	untouched("the mark", synced.ID, syncedSeen)
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, synced.ID, strike.Add(time.Hour))
	check("mark again", ok, err, false)
	if got := mustFindItem(t, srv, user, synced.ID); got.IMDbMissingSince == nil || !got.IMDbMissingSince.Equal(strike) {
		t.Fatalf("imdb_missing_since = %v, want the first strike %v", got.IMDbMissingSince, strike)
	}

	// The delete re-checks everything at the moment it runs. Each call below
	// passes the updated_at a fresh plan would carry, so that it is the
	// condition under test that refuses, except where the point is a plan
	// that has gone stale.
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, manual.ID, strike, seen(manual.ID))
	check("delete an item the sync did not add", ok, err, false)
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, stranger, synced.ID, strike, seen(synced.ID))
	check("delete someone else's item", ok, err, false)
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, synced.ID, strike.Add(time.Hour), seen(synced.ID))
	check("delete with a stale mark", ok, err, false)
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, syncedToo.ID, strike, seen(syncedToo.ID))
	check("delete an item that is not marked", ok, err, false)

	// An edit that leaves status and tags alone still wins against a delete
	// planned before it. updated_at has millisecond resolution, hence the pause.
	plannedAt := seen(synced.ID)
	time.Sleep(5 * time.Millisecond)
	edit(synced.ID, map[string]any{"title": "Synced, renamed"})
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, synced.ID, strike, plannedAt)
	check("delete planned before the user renamed the item", ok, err, false)
	if got := mustFindItem(t, srv, user, synced.ID); got == nil || got.Title != "Synced, renamed" || got.IMDbMissingSince == nil {
		t.Fatalf("item after the refused delete = %+v; want it kept, renamed and still marked", got)
	}

	edit(synced.ID, map[string]any{"status": models.WatchlistStatusWatching})
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, synced.ID, strike, seen(synced.ID))
	check("delete after the user changed the status", ok, err, false)

	edit(synced.ID, map[string]any{"status": models.WatchlistStatusPlanned, "tags": []string{"keep"}})
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, synced.ID, strike, seen(synced.ID))
	check("delete after the user tagged it", ok, err, false)
	if mustFindItem(t, srv, user, synced.ID) == nil {
		t.Fatal("a refused delete removed the item")
	}

	edit(synced.ID, map[string]any{"tags": []string{}})
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, synced.ID, strike, seen(synced.ID))
	check("delete an untouched, marked, sync-added item", ok, err, true)
	if mustFindItem(t, srv, user, synced.ID) != nil {
		t.Error("the item is still there after the delete")
	}

	// Seeing the title again clears the mark, and with it the way to a delete.
	ok, err = srv.ClearWatchlistItemIMDbMissing(ctx, user, syncedToo.ID)
	check("clear an item that is not marked", ok, err, false)
	tooSeen := seen(syncedToo.ID)
	ok, err = srv.MarkWatchlistItemIMDbMissing(ctx, user, syncedToo.ID, strike)
	check("mark the second item", ok, err, true)
	ok, err = srv.ClearWatchlistItemIMDbMissing(ctx, stranger, syncedToo.ID)
	check("clear someone else's item", ok, err, false)
	ok, err = srv.ClearWatchlistItemIMDbMissing(ctx, user, syncedToo.ID)
	check("clear", ok, err, true)
	untouched("marking and clearing", syncedToo.ID, tooSeen)
	ok, err = srv.DeleteSyncedWatchlistItem(ctx, user, syncedToo.ID, strike, tooSeen)
	check("delete after the mark was cleared", ok, err, false)
	if got := mustFindItem(t, srv, user, syncedToo.ID); got == nil || got.IMDbMissingSince != nil {
		t.Errorf("item after clear = %+v; want it present and unmarked", got)
	}
}

// Unlinking and re-linking wipe every strike of one user in a single update,
// and of nobody else.
func TestClearIMDbMissingMarks(t *testing.T) {
	srv := New()
	ctx := context.Background()
	user, other := primitive.NewObjectID(), primitive.NewObjectID()
	strike := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Millisecond)

	seed := func(owner primitive.ObjectID, base int) []*models.WatchlistItem {
		t.Helper()
		created, err := srv.CreateWatchlistItems(ctx, []*models.WatchlistItem{
			{UserID: owner, Title: "Marked", Type: models.WatchlistTypeMovie, TMDbID: base, IMDbID: fmt.Sprintf("tt%07d", base), Source: models.SourceIMDb},
			{UserID: owner, Title: "Marked Too", Type: models.WatchlistTypeMovie, TMDbID: base + 1, IMDbID: fmt.Sprintf("tt%07d", base+1), Source: models.SourceIMDb},
			{UserID: owner, Title: "Unmarked", Type: models.WatchlistTypeMovie, TMDbID: base + 2, IMDbID: fmt.Sprintf("tt%07d", base+2), Source: models.SourceIMDb},
		})
		if err != nil || len(created) != 3 {
			t.Fatalf("seed: created = %d, err = %v", len(created), err)
		}
		for _, item := range created[:2] {
			if ok, err := srv.MarkWatchlistItemIMDbMissing(ctx, owner, item.ID, strike); err != nil || !ok {
				t.Fatalf("mark: applied = %v, err = %v", ok, err)
			}
		}
		return created
	}
	mine, theirs := seed(user, 900500), seed(other, 900510)
	updatedAt := mustFindItem(t, srv, user, mine[0].ID).UpdatedAt

	n, err := srv.ClearIMDbMissingMarks(ctx, user)
	if err != nil || n != 2 {
		t.Fatalf("cleared = %d, err = %v; want the user's two marks", n, err)
	}
	for _, item := range mine {
		got := mustFindItem(t, srv, user, item.ID)
		if got == nil || got.IMDbMissingSince != nil {
			t.Errorf("%s after the clear = %+v; want it present and unmarked", item.Title, got)
		}
	}
	if got := mustFindItem(t, srv, user, mine[0].ID).UpdatedAt; !got.Equal(updatedAt) {
		t.Errorf("clearing moved updated_at from %v to %v", updatedAt, got)
	}
	for _, item := range theirs[:2] {
		got := mustFindItem(t, srv, other, item.ID)
		if got == nil || got.IMDbMissingSince == nil || !got.IMDbMissingSince.Equal(strike) {
			t.Errorf("another user's %s = %+v; want its mark left alone", item.Title, got)
		}
	}

	// A removal planned on a strike that has since been wiped does not happen.
	ok, err := srv.DeleteSyncedWatchlistItem(ctx, user, mine[0].ID, strike, updatedAt)
	if err != nil || ok {
		t.Errorf("delete after the clear: applied = %v, err = %v; want refused", ok, err)
	}
	if n, err := srv.ClearIMDbMissingMarks(ctx, user); err != nil || n != 0 {
		t.Errorf("second clear = %d, err = %v; want nothing left to clear", n, err)
	}
}

func TestGetTitlesByIMDbIDs(t *testing.T) {
	srv := New()
	ctx := context.Background()
	save := func(ref models.TitleRef, meta *models.TitleMetadata) {
		t.Helper()
		if ok, err := srv.SaveTitleSnapshot(ctx, ref, "US", nil, snapshot(t1, "Netflix"), meta); err != nil || !ok {
			t.Fatalf("SaveTitleSnapshot %s: applied = %v, err = %v", ref.Key(), ok, err)
		}
	}
	known := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 900400}
	gone := models.TitleRef{MediaType: models.MediaTypeTV, TMDbID: 900401}
	save(known, &models.TitleMetadata{Title: "Known", Year: 1999, IMDbID: "tt9004000"})
	save(gone, &models.TitleMetadata{Title: "Gone", IMDbID: "tt9004001"})
	save(models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 900402}, nil) // no IMDb ID stored
	if err := srv.MarkTitleNotFound(ctx, gone, t1); err != nil {
		t.Fatal(err)
	}

	got, err := srv.GetTitlesByIMDbIDs(ctx, []string{"tt9004000", "tt9004001", "tt9004999"})
	if err != nil {
		t.Fatalf("GetTitlesByIMDbIDs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("found %d titles, want only the known one: %v", len(got), got)
	}
	title := got["tt9004000"]
	if title == nil || title.ID != known.Key() || title.MediaType != models.MediaTypeMovie || title.TMDbID != 900400 || title.Title != "Known" || title.Year != 1999 {
		t.Errorf("title = %+v", title)
	}
	if title != nil && len(title.Providers) != 0 {
		t.Errorf("provider snapshots were loaded: %v", title.Providers)
	}
	if none, err := srv.GetTitlesByIMDbIDs(ctx, nil); err != nil || len(none) != 0 {
		t.Errorf("no IDs: got %v, err = %v", none, err)
	}
}

func TestIMDbLink(t *testing.T) {
	srv := New()
	ctx := context.Background()
	const email = "it-imdb-link@example.com"
	load := func() *models.User {
		t.Helper()
		u, err := srv.GetUserByEmail(ctx, email)
		if err != nil || u == nil {
			t.Fatalf("GetUserByEmail: %v, %v", u, err)
		}
		return u
	}
	listed := func(attemptedBefore time.Time, id primitive.ObjectID) bool {
		t.Helper()
		users, err := srv.ListIMDbLinkedUsers(ctx, attemptedBefore)
		if err != nil {
			t.Fatalf("ListIMDbLinkedUsers: %v", err)
		}
		for _, u := range users {
			if u.IMDbSync == nil {
				t.Errorf("a user without a link was listed: %s", u.ID.Hex())
			}
			if u.ID == id {
				return true
			}
		}
		return false
	}
	claim := func(what string, id primitive.ObjectID, at time.Time, want bool) {
		t.Helper()
		got, err := srv.ClaimIMDbAttempt(ctx, id, at, time.Minute)
		if err != nil || got != want {
			t.Errorf("%s: claimed = %v, err = %v; want %v", what, got, err, want)
		}
	}

	if err := srv.UpsertUser(ctx, &models.User{Email: email, Name: "Link"}); err != nil {
		t.Fatal(err)
	}
	user := load()
	if user.IMDbSync != nil || user.IMDbAttemptAt != nil {
		t.Fatalf("a new user has IMDb state: %+v", user)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	far := now.Add(24 * time.Hour)
	if listed(far, user.ID) {
		t.Error("a user without a link is due for a sync")
	}

	// The fetch throttle.
	claim("first claim", user.ID, now, true)
	claim("claim inside the gap", user.ID, now.Add(30*time.Second), false)
	claim("claim of an unknown user", primitive.NewObjectID(), now, false)
	attempt := now.Add(61 * time.Second)
	claim("claim after the gap", user.ID, attempt, true)
	if got := load().IMDbAttemptAt; got == nil || !got.Equal(attempt) {
		t.Errorf("imdb_attempt_at = %v, want %v", got, attempt)
	}

	// Linking.
	sync := models.IMDbSync{
		SourceID: "ls900000001", LinkedAt: now, LastAttemptAt: &attempt, LastSyncedAt: &attempt,
		LastStatus: models.IMDbStatusOK, Total: 3, Added: 2, Unmatched: 1,
	}
	if err := srv.LinkIMDb(ctx, primitive.NewObjectID(), sync); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("linking an unknown user: err = %v, want ErrUserNotFound", err)
	}
	if err := srv.LinkIMDb(ctx, user.ID, sync); err != nil {
		t.Fatalf("LinkIMDb: %v", err)
	}
	stored := load().IMDbSync
	if stored == nil || stored.SourceID != sync.SourceID || !stored.LinkedAt.Equal(now) ||
		stored.LastAttemptAt == nil || !stored.LastAttemptAt.Equal(attempt) ||
		stored.LastSyncedAt == nil || !stored.LastSyncedAt.Equal(attempt) ||
		stored.LastStatus != models.IMDbStatusOK || stored.LastError != "" ||
		stored.Total != 3 || stored.Added != 2 || stored.Removed != 0 || stored.Unmatched != 1 {
		t.Errorf("stored link = %+v, want %+v", stored, sync)
	}

	// Who is due: linked, and not attempted after the cutoff.
	if listed(attempt.Add(-time.Second), user.ID) {
		t.Error("a user attempted after the cutoff is due")
	}
	if !listed(attempt, user.ID) {
		t.Error("a linked user attempted at the cutoff is not due")
	}

	// A run's outcome is stored only while the user is still linked to its list.
	failed := sync
	failed.LastStatus, failed.LastError, failed.LastSyncedAt = models.IMDbStatusFailed, "imdb: status 503", nil
	if ok, err := srv.UpdateIMDbSync(ctx, user.ID, failed); err != nil || !ok {
		t.Errorf("UpdateIMDbSync: applied = %v, err = %v", ok, err)
	}
	elsewhere := failed
	elsewhere.SourceID, elsewhere.LastStatus = "ur9000002", models.IMDbStatusOK
	if ok, err := srv.UpdateIMDbSync(ctx, user.ID, elsewhere); err != nil || ok {
		t.Errorf("outcome for another list: applied = %v, err = %v; want dropped", ok, err)
	}
	// The same list linked at another moment is another link: the user
	// unlinked and linked it again while the run was under way.
	relinked := failed
	relinked.LinkedAt, relinked.LastStatus = now.Add(-time.Hour), models.IMDbStatusOK
	if ok, err := srv.UpdateIMDbSync(ctx, user.ID, relinked); err != nil || ok {
		t.Errorf("outcome for an earlier link to the same list: applied = %v, err = %v; want dropped", ok, err)
	}
	if got := load().IMDbSync; got.SourceID != sync.SourceID || !got.LinkedAt.Equal(now) || got.LastStatus != models.IMDbStatusFailed || got.LastError != "imdb: status 503" || got.LastSyncedAt != nil {
		t.Errorf("link after the updates = %+v", got)
	}

	// Reading the link on its own, as a run does before it removes anything.
	link, err := srv.GetIMDbLink(ctx, user.ID)
	if err != nil || link == nil || link.SourceID != sync.SourceID || !link.LinkedAt.Equal(now) || link.LastStatus != models.IMDbStatusFailed {
		t.Errorf("GetIMDbLink = %+v, err = %v", link, err)
	}
	if link, err := srv.GetIMDbLink(ctx, primitive.NewObjectID()); err != nil || link != nil {
		t.Errorf("link of an unknown user = %+v, err = %v; want none", link, err)
	}

	// Signing in again (an upsert of the profile) leaves the link alone.
	if err := srv.UpsertUser(ctx, &models.User{Email: email, Name: "Renamed"}); err != nil {
		t.Fatal(err)
	}
	if got := load(); got.IMDbSync == nil || got.Name != "Renamed" {
		t.Errorf("after signing in again: %+v", got)
	}

	// Unlinking removes the link and keeps the throttle.
	for i := 0; i < 2; i++ {
		if err := srv.UnlinkIMDb(ctx, user.ID); err != nil {
			t.Fatalf("UnlinkIMDb, call %d: %v", i+1, err)
		}
	}
	unlinked := load()
	if unlinked.IMDbSync != nil {
		t.Errorf("link survived unlinking: %+v", unlinked.IMDbSync)
	}
	if unlinked.IMDbAttemptAt == nil || !unlinked.IMDbAttemptAt.Equal(attempt) {
		t.Errorf("imdb_attempt_at after unlinking = %v, want it kept at %v", unlinked.IMDbAttemptAt, attempt)
	}
	claim("claim right after unlinking", user.ID, attempt.Add(10*time.Second), false)
	if listed(far, user.ID) {
		t.Error("an unlinked user is due for a sync")
	}
	if ok, err := srv.UpdateIMDbSync(ctx, user.ID, failed); err != nil || ok {
		t.Errorf("outcome after unlinking: applied = %v, err = %v; want dropped", ok, err)
	}
	if load().IMDbSync != nil {
		t.Error("a late outcome linked the user again")
	}
	if link, err := srv.GetIMDbLink(ctx, user.ID); err != nil || link != nil {
		t.Errorf("link after unlinking = %+v, err = %v; want none", link, err)
	}
}

// Users due for a sync come back longest wait first, never-fetched users
// before everyone, so a run that runs out of time does not starve anyone.
func TestListIMDbLinkedUsersOrder(t *testing.T) {
	srv := New()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Created in the opposite order to the one expected back.
	attempts := []struct {
		email string
		ago   time.Duration // zero: never fetched
	}{
		{"it-imdb-order-recent@example.com", 48 * time.Hour},
		{"it-imdb-order-oldest@example.com", 72 * time.Hour},
		{"it-imdb-order-never@example.com", 0},
	}
	position := map[primitive.ObjectID]string{}
	for i, a := range attempts {
		if err := srv.UpsertUser(ctx, &models.User{Email: a.email, Name: "Order"}); err != nil {
			t.Fatal(err)
		}
		user, err := srv.GetUserByEmail(ctx, a.email)
		if err != nil || user == nil {
			t.Fatalf("GetUserByEmail: %v, %v", user, err)
		}
		position[user.ID] = a.email
		if a.ago > 0 {
			if ok, err := srv.ClaimIMDbAttempt(ctx, user.ID, now.Add(-a.ago), 0); err != nil || !ok {
				t.Fatalf("ClaimIMDbAttempt: %v, %v", ok, err)
			}
		}
		link := models.IMDbSync{SourceID: fmt.Sprintf("ls90000010%d", i), LinkedAt: now}
		if err := srv.LinkIMDb(ctx, user.ID, link); err != nil {
			t.Fatalf("LinkIMDb: %v", err)
		}
	}

	users, err := srv.ListIMDbLinkedUsers(ctx, now)
	if err != nil {
		t.Fatalf("ListIMDbLinkedUsers: %v", err)
	}
	var got []string
	for _, u := range users {
		if email, mine := position[u.ID]; mine {
			got = append(got, email)
		}
	}
	want := []string{attempts[2].email, attempts[1].email, attempts[0].email}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
