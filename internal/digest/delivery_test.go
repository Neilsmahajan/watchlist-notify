package digest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/availability"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const alienKey = "movie:348"

// addAlien puts a second title on the world's user's list, streaming on
// Netflix, and returns the event for its arrival there at the given time.
func (w *world) addAlien(detectedAt time.Time) models.AvailabilityEvent {
	w.store.AddItem(&models.WatchlistItem{
		UserID: w.user.ID, Title: "Alien", Type: models.WatchlistTypeMovie, Year: 1979, TMDbID: 348,
		Status: models.WatchlistStatusPlanned, AddedAt: day(-30),
	})
	w.store.PutTitle(streamingOn(alienKey, models.ServiceNetflix))
	ev := added(models.ServiceNetflix, detectedAt)
	ev.TitleKey = alienKey
	ev.ID = alienKey + ev.ID
	return ev
}

// runAt runs a send-mode digest for the world's user as of the given time.
func (w *world) runAt(at time.Time) Summary {
	w.t.Helper()
	r := w.runner(ModeSend, w.user.Email)
	r.now = func() time.Time { return at }
	return w.run(r)
}

// announced reports which of the given titles the nth email mentions.
func (w *world) announced(n int, titles ...string) []string {
	w.t.Helper()
	if n >= len(w.mailer.sent) {
		w.t.Fatalf("only %d emails were sent, want at least %d", len(w.mailer.sent), n+1)
	}
	var found []string
	for _, title := range titles {
		if strings.Contains(w.mailer.sent[n].html, title) {
			found = append(found, title)
		}
	}
	return found
}

// An event carries the time its change was detected, but only becomes
// readable when the write commits. A digest that runs in between moves the
// user's cursor past an event it never saw.
func TestRunDeliversAnEventCommittedAfterTheDigestThatShouldHaveSeenIt(t *testing.T) {
	w := newWorld(t)

	// Detected a minute before the first digest, not yet committed.
	late := w.addAlien(now.Add(-time.Minute))

	if sum := w.runAt(now); sum.Sent != 1 || sum.Titles != 1 {
		t.Fatalf("first digest = %+v, want one title sent", sum)
	}
	if got, want := w.announced(0, "The Matrix", "Alien"), []string{"The Matrix"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first digest announced %v, want %v", got, want)
	}

	// The write lands. Its detection time is now behind last_sent_at.
	w.store.AddEvent(late)

	if sum := w.runAt(day(7)); sum.Sent != 1 || sum.Titles != 1 {
		t.Fatalf("second digest = %+v, want the late event sent", sum)
	}
	if got, want := w.announced(1, "The Matrix", "Alien"), []string{"Alien"}; !reflect.DeepEqual(got, want) {
		t.Errorf("second digest announced %v, want %v and nothing repeated", got, want)
	}
	if got := w.store.Deliveries(w.user.ID); len(got) != 2 {
		t.Errorf("delivery records = %v, want one per event", got)
	}
}

func TestRunDoesNotRepeatADeliveredEventInsideTheOverlap(t *testing.T) {
	w := newWorld(t)
	if sum := w.runAt(now); sum.Sent != 1 {
		t.Fatalf("first digest = %+v", sum)
	}

	// Every later digest looks back over the event; none may announce it again.
	for _, at := range []time.Time{day(7), day(8), day(14)} {
		sum := w.runAt(at)
		if sum.Sent != 0 || sum.Recipients() != 0 {
			t.Errorf("digest at %v = %+v, want nothing to send", at, sum)
		}
	}
	if len(w.mailer.sent) != 1 {
		t.Errorf("the user was emailed %d times about one arrival", len(w.mailer.sent))
	}
}

// brokenDeliveries fails writes to the delivery log.
type brokenDeliveries struct {
	*storetest.Store
	insertErr, deleteErr error
}

func (s *brokenDeliveries) InsertDigestDeliveries(ctx context.Context, d []models.DigestDelivery) (int, error) {
	if s.insertErr != nil {
		return 0, s.insertErr
	}
	return s.Store.InsertDigestDeliveries(ctx, d)
}

func (s *brokenDeliveries) DeleteDigestDeliveries(ctx context.Context, userID primitive.ObjectID, ids []string) (int, error) {
	if s.deleteErr != nil {
		return 0, s.deleteErr
	}
	return s.Store.DeleteDigestDeliveries(ctx, userID, ids)
}

// Without a delivery record the next digest would repeat the email, so a
// digest that cannot record its deliveries is not sent.
func TestRunDoesNotSendWhenDeliveriesCannotBeRecorded(t *testing.T) {
	w := newWorld(t)
	broken := &brokenDeliveries{Store: w.store, insertErr: errors.New("write concern timeout")}

	sum, err := w.runnerOn(broken, ModeSend, w.user.Email).Run(context.Background())

	if err == nil || sum.Errors != 1 || sum.Sent != 0 {
		t.Errorf("Run = %+v, %v; want the user counted as an error", sum, err)
	}
	if len(w.mailer.sent) != 0 {
		t.Error("an email went out without a delivery record")
	}
	if s := w.schedule(); s.LastSentAt != nil || s.NextScheduledAt != nil {
		t.Errorf("schedule = %+v, want the claim released so the next run retries", s)
	}

	if sum := w.runAt(now); sum.Sent != 1 || len(w.mailer.sent) != 1 {
		t.Errorf("retry = %+v, emails = %d; want it sent", sum, len(w.mailer.sent))
	}
}

// If the records of a failed send cannot be removed, the events they name are
// lost to this user, but the user must not be left waiting a whole interval
// for whatever arrives next.
func TestRunReleasesTheClaimEvenWhenRecordsCannotBeRemoved(t *testing.T) {
	w := newWorld(t)
	w.mailer.err = &notifications.RejectedError{StatusCode: 422}
	broken := &brokenDeliveries{Store: w.store, deleteErr: errors.New("connection reset")}

	sum, err := w.runnerOn(broken, ModeSend, w.user.Email).Run(context.Background())

	if err != nil || sum.SendFailed != 1 {
		t.Errorf("Run = %+v, %v", sum, err)
	}
	if s := w.schedule(); s.LastSentAt != nil || s.NextScheduledAt != nil {
		t.Errorf("schedule = %+v, want the claim released", s)
	}
	if !strings.Contains(w.logs.String(), "delivery records not removed") {
		t.Error("the leftover records were not logged")
	}
}

// --- the refresher and the digest together ---

// titleFetcher answers TMDb lookups from a table of title key to providers.
type titleFetcher map[string][]tmdb.Provider

func (f titleFetcher) GetTitle(_ context.Context, mediaType string, id int) (*tmdb.TitleDetails, error) {
	ref := models.TitleRef{MediaType: mediaType, TMDbID: id}
	return &tmdb.TitleDetails{
		ID: id, MediaType: mediaType, Title: ref.Key(),
		Providers: map[string]tmdb.RegionProviders{"US": {Flatrate: f[ref.Key()]}},
	}, nil
}

// failingSave fails the snapshot write for one title until it is told to stop,
// as a database blip between recording events and replacing the snapshot does.
type failingSave struct {
	*storetest.Store
	key string
}

func (s *failingSave) SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prev *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error) {
	if ref.Key() == s.key {
		return false, errors.New("write concern timeout")
	}
	return s.Store.SaveTitleSnapshot(ctx, ref, region, prev, snap, meta)
}

// The refresh writes an event and then the snapshot it belongs to. When the
// snapshot write fails, the event exists but is not true yet: the digest
// rightly leaves the title out. If another title is emailed meanwhile, the
// user's cursor moves past the event. When the retry lands, the event keeps
// its original detection time, now behind the cursor, and must still reach
// the user: once.
func TestSnapshotSaveFailingThenSucceedingEmailsTheTitleOnce(t *testing.T) {
	w := &world{t: t, store: storetest.New(), mailer: &fakeMailer{}, logs: &bytes.Buffer{}}
	user := subscriber(models.ServiceNetflix)
	user.Preferences.Digest = models.DigestSettings{Enabled: true, Interval: 1, IntervalUnit: models.DigestUnitDays}
	w.user = w.store.AddUser(user)

	matrix := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 603}
	alien := models.TitleRef{MediaType: models.MediaTypeMovie, TMDbID: 348}
	refs := []models.TitleRef{matrix, alien}
	for _, item := range []*models.WatchlistItem{
		{Title: "The Matrix", Type: models.WatchlistTypeMovie, TMDbID: 603},
		{Title: "Alien", Type: models.WatchlistTypeMovie, TMDbID: 348},
	} {
		item.UserID, item.Status, item.AddedAt = user.ID, models.WatchlistStatusPlanned, day(-30)
		w.store.AddItem(item)
	}

	fetcher := titleFetcher{}
	refreshAt := func(store availability.Store, at time.Time) availability.Summary {
		t.Helper()
		logger := slog.New(slog.NewTextHandler(w.logs, nil))
		r := availability.NewRefresher(fetcher, store, logger, availability.WithClock(func() time.Time { return at }))
		sum, err := r.RefreshAll(context.Background(), refs, []string{"US"})
		if err != nil {
			t.Fatalf("RefreshAll: %v", err)
		}
		return sum
	}

	// Baselines: neither title streams anywhere yet.
	if sum := refreshAt(w.store, day(-2)); sum.Baselines != 2 || sum.Events != 0 {
		t.Fatalf("baseline refresh = %+v", sum)
	}

	// Both arrive on Netflix. The Matrix's snapshot write fails.
	netflix := []tmdb.Provider{{ProviderID: 8, ProviderName: "Netflix"}}
	fetcher[matrix.Key()], fetcher[alien.Key()] = netflix, netflix
	detectedAt := now.Add(-time.Hour)
	if sum := refreshAt(&failingSave{Store: w.store, key: matrix.Key()}, detectedAt); sum.Failed != 1 || sum.Refreshed != 1 {
		t.Fatalf("refresh with a failing save = %+v, want one title refreshed and one failed", sum)
	}
	if got := len(w.store.Events()); got != 2 {
		t.Fatalf("%d events recorded, want both: the event is written before the snapshot", got)
	}

	// Today's digest announces Alien only, and the cursor moves to now.
	if sum := w.runAt(now); sum.Sent != 1 || sum.Titles != 1 {
		t.Fatalf("first digest = %+v", sum)
	}
	if got, want := w.announced(0, "The Matrix", "Alien"), []string{"Alien"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first digest announced %v, want %v", got, want)
	}

	// Tomorrow's refresh retries. Same change, same event ID, so the event
	// that already exists is kept with its original detection time.
	if sum := refreshAt(w.store, day(1).Add(-time.Hour)); sum.Failed != 0 || sum.Events != 0 {
		t.Fatalf("retry refresh = %+v, want no failures and no new events", sum)
	}
	for _, ev := range w.store.Events() {
		if ev.TitleKey == matrix.Key() && !ev.DetectedAt.Equal(detectedAt) {
			t.Fatalf("the retried event was re-stamped to %v, want the original %v", ev.DetectedAt, detectedAt)
		}
	}

	// Tomorrow's digest picks The Matrix up although it is behind the cursor.
	if sum := w.runAt(day(1)); sum.Sent != 1 || sum.Titles != 1 {
		t.Fatalf("second digest = %+v, want the retried title sent", sum)
	}
	if got, want := w.announced(1, "The Matrix", "Alien"), []string{"The Matrix"}; !reflect.DeepEqual(got, want) {
		t.Errorf("second digest announced %v, want %v", got, want)
	}

	// And nothing is said twice afterwards.
	for _, at := range []time.Time{day(2), day(3), day(8)} {
		refreshAt(w.store, at.Add(-time.Hour))
		if sum := w.runAt(at); sum.Recipients() != 0 {
			t.Errorf("digest at %v = %+v, want nothing to send", at, sum)
		}
	}
	if len(w.mailer.sent) != 2 {
		t.Errorf("%d emails were sent, want 2: each title exactly once", len(w.mailer.sent))
	}
}
