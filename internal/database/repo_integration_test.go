package database

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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
