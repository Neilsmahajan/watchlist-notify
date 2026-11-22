package digest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"github.com/neilsmahajan/watchlist-notify/internal/storetest"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// sentEmail is one call to the fake mailer.
type sentEmail struct{ to, subject, html string }

// fakeMailer records sends and fails on demand.
type fakeMailer struct {
	sent   []sentEmail
	err    error
	onSend func() // runs inside SendEmail, before it returns
}

func (m *fakeMailer) SendEmail(to, subject, html string) error {
	if m.onSend != nil {
		m.onSend()
	}
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentEmail{to, subject, html})
	return nil
}

// world is a store holding one due user whose watchlist has one title that
// arrived on Netflix yesterday, plus the pieces to run digests against it.
type world struct {
	t      *testing.T
	store  *storetest.Store
	mailer *fakeMailer
	user   *models.User
	logs   *bytes.Buffer
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, store: storetest.New(), mailer: &fakeMailer{}, logs: &bytes.Buffer{}}

	w.user = w.store.AddUser(subscriber(models.ServiceNetflix))
	item := matrixItem(day(-30))
	item.UserID = w.user.ID
	w.store.AddItem(item)
	w.store.PutTitle(streamingOn(matrixKey, models.ServiceNetflix))
	w.store.AddEvent(added(models.ServiceNetflix, day(-1)))
	return w
}

func (w *world) runner(mode Mode, allowlist string) *Runner {
	w.t.Helper()
	return w.runnerOn(w.store, mode, allowlist)
}

func (w *world) runnerOn(store Store, mode Mode, allowlist string) *Runner {
	w.t.Helper()
	r, err := NewRunner(store, w.mailer, Config{
		Mode:      mode,
		Allowlist: ParseAllowlist(allowlist),
		SiteURL:   "https://watchlistnotify.com",
	}, slog.New(slog.NewTextHandler(w.logs, nil)))
	if err != nil {
		w.t.Fatal(err)
	}
	r.now = func() time.Time { return now }
	return r
}

func (w *world) run(r *Runner) Summary {
	w.t.Helper()
	sum, err := r.Run(context.Background())
	if err != nil {
		w.t.Fatalf("Run: %v", err)
	}
	return sum
}

// schedule returns the user's stored digest schedule.
func (w *world) schedule() models.DigestSchedule {
	return w.store.User(w.user.ID).Preferences.Digest.Schedule()
}

func (w *world) assertUntouched() {
	w.t.Helper()
	if w.store.Writes != 0 {
		w.t.Errorf("the store was written %d times, want 0", w.store.Writes)
	}
	if s := w.schedule(); s.LastSentAt != nil || s.NextScheduledAt != nil {
		w.t.Errorf("schedule = %+v, want it untouched so the user stays due", s)
	}
}

func TestRunDryRunSendsAndWritesNothing(t *testing.T) {
	w := newWorld(t)

	// Even with the user allowlisted, a dry run is a dry run.
	sum := w.run(w.runner(ModeDryRun, w.user.Email))

	if sum.Due != 1 || sum.DryRun != 1 || sum.Titles != 1 || sum.Recipients() != 1 || sum.Sent != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if len(w.mailer.sent) != 0 {
		t.Errorf("a dry run sent %d emails", len(w.mailer.sent))
	}
	w.assertUntouched()
}

func TestRunDefaultsToDryRun(t *testing.T) {
	w := newWorld(t)
	sum := w.run(w.runner("", w.user.Email))
	if sum.Mode != ModeDryRun || sum.DryRun != 1 || len(w.mailer.sent) != 0 {
		t.Errorf("zero-value mode: summary = %+v, emails = %d", sum, len(w.mailer.sent))
	}
	w.assertUntouched()
}

func TestRunNeverEmailsUsersOffTheAllowlist(t *testing.T) {
	allowlists := map[string]string{
		"empty allowlist":      "",
		"someone else":         "other@example.com",
		"notification address": "alerts@example.com", // the account must be listed too
	}
	for name, allowlist := range allowlists {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			sum := w.run(w.runner(ModeSend, allowlist))

			if sum.NotAllowlisted != 1 || sum.Sent != 0 || sum.Titles != 1 {
				t.Errorf("summary = %+v, want the user counted as not allowlisted", sum)
			}
			if len(w.mailer.sent) != 0 {
				t.Errorf("sent %d emails to a user who is not allowlisted", len(w.mailer.sent))
			}
			w.assertUntouched()
		})
	}
}

func TestRunSendsToAllowlistedUsers(t *testing.T) {
	w := newWorld(t)
	other := subscriber(models.ServiceNetflix)
	other.Email = "bystander@example.com"
	w.store.AddUser(other)
	otherItem := matrixItem(day(-30))
	otherItem.UserID = other.ID
	w.store.AddItem(otherItem)

	// The allowlist is matched ignoring case and surrounding space.
	sum := w.run(w.runner(ModeSend, " Viewer@Example.com , someone@example.com"))

	if sum.Due != 2 || sum.Sent != 1 || sum.NotAllowlisted != 1 || sum.Titles != 2 {
		t.Errorf("summary = %+v", sum)
	}
	if len(w.mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(w.mailer.sent))
	}
	email := w.mailer.sent[0]
	if email.to != "viewer@example.com" {
		t.Errorf("sent to %q", email.to)
	}
	if email.subject != "1 title on your watchlist is now streaming" || !strings.Contains(email.html, "Now on Netflix") {
		t.Errorf("unexpected email: subject %q", email.subject)
	}

	s := w.schedule()
	if s.LastSentAt == nil || !s.LastSentAt.Equal(now) {
		t.Errorf("last_sent_at = %v, want %v", s.LastSentAt, now)
	}
	if s.NextScheduledAt == nil || !s.NextScheduledAt.Equal(day(7)) {
		t.Errorf("next_scheduled_at = %v, want one interval on, %v", s.NextScheduledAt, day(7))
	}
	if bystander := w.store.User(other.ID).Preferences.Digest; bystander.LastSentAt != nil {
		t.Error("the schedule of a user who was not emailed moved")
	}

	// The next day's run finds the sender no longer due.
	again := w.run(w.runner(ModeSend, "viewer@example.com"))
	if again.Due != 1 || again.Sent != 0 || len(w.mailer.sent) != 1 {
		t.Errorf("second run: summary = %+v, emails = %d", again, len(w.mailer.sent))
	}
}

// notifyAt points the world's user at a separate notification address.
func (w *world) notifyAt(address string) {
	u := w.store.User(w.user.ID)
	u.Preferences.NotifyEmail = address
	u.Preferences.UseAccountEmail = false
	w.store.AddUser(u)
}

func TestRunSendsToAnAllowlistedNotificationAddress(t *testing.T) {
	w := newWorld(t)
	w.notifyAt(" Alerts@Example.com ")

	// The account and the address the mail goes to are both listed.
	sum := w.run(w.runner(ModeSend, "viewer@example.com,alerts@example.com"))

	if sum.Sent != 1 {
		t.Errorf("summary = %+v", sum)
	}
	if len(w.mailer.sent) != 1 || w.mailer.sent[0].to != "Alerts@Example.com" {
		t.Errorf("sent = %+v, want one email to the notification address", w.mailer.sent)
	}
}

// The notification address is typed in by the user and never verified. An
// allowlisted account must not be able to aim its digest at an address that
// is not on the list.
func TestRunNeverEmailsANotificationAddressOffTheAllowlist(t *testing.T) {
	w := newWorld(t)
	w.notifyAt("stranger@example.net")

	sum := w.run(w.runner(ModeSend, "viewer@example.com"))

	if sum.NotAllowlisted != 1 || sum.Sent != 0 || sum.Titles != 1 {
		t.Errorf("summary = %+v, want the user counted as not allowlisted", sum)
	}
	if len(w.mailer.sent) != 0 {
		t.Errorf("sent %d emails to an address that is not allowlisted", len(w.mailer.sent))
	}
	w.assertUntouched()

	// Switching back to the account email makes the same user deliverable.
	u := w.store.User(w.user.ID)
	u.Preferences.UseAccountEmail = true
	w.store.AddUser(u)
	if sum := w.run(w.runner(ModeSend, "viewer@example.com")); sum.Sent != 1 || w.mailer.sent[0].to != "viewer@example.com" {
		t.Errorf("summary = %+v, sent = %+v; want one email to the account address", sum, w.mailer.sent)
	}
}

func TestRunWithNothingNewLeavesTheUserDue(t *testing.T) {
	w := newWorld(t)
	// The only arrival is older than the window.
	stale := storetest.New()
	user := stale.AddUser(subscriber(models.ServiceNetflix))
	item := matrixItem(day(-30))
	item.UserID = user.ID
	stale.AddItem(item)
	stale.PutTitle(streamingOn(matrixKey, models.ServiceNetflix))
	stale.AddEvent(added(models.ServiceNetflix, day(-20)))
	w.store, w.user = stale, user

	sum := w.run(w.runner(ModeSend, user.Email))

	if sum.Due != 1 || sum.NothingNew != 1 || sum.Recipients() != 0 || sum.Titles != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if len(w.mailer.sent) != 0 {
		t.Error("an empty digest was sent")
	}
	w.assertUntouched()

	// Still due tomorrow, and told as soon as something arrives.
	stale.AddEvent(added(models.ServiceNetflix, now.Add(12*time.Hour)))
	r := w.runner(ModeSend, user.Email)
	r.now = func() time.Time { return day(1) }
	if sum := w.run(r); sum.Sent != 1 {
		t.Errorf("next day's summary = %+v, want the digest sent", sum)
	}
}

// staleReads serves the list of due users, and the delivery log, as they were
// before anyone claimed the users: the way a second job run that started at
// the same moment sees them.
type staleReads struct {
	*storetest.Store
	due []*models.User
}

func (s *staleReads) GetUsersForDigest(context.Context, time.Time) ([]*models.User, error) {
	return s.due, nil
}

func (s *staleReads) ListDeliveredEventIDs(context.Context, primitive.ObjectID, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func TestRunClaimPreventsDoubleSend(t *testing.T) {
	w := newWorld(t)
	due, err := w.store.GetUsersForDigest(context.Background(), now)
	if err != nil || len(due) != 1 {
		t.Fatalf("GetUsersForDigest = %v, %v", due, err)
	}
	overlapping := w.runnerOn(&staleReads{Store: w.store, due: due}, ModeSend, w.user.Email)

	first := w.run(w.runner(ModeSend, w.user.Email))
	second := w.run(overlapping) // still believes the user is due

	if first.Sent != 1 {
		t.Errorf("first run = %+v, want it to send", first)
	}
	if second.Sent != 0 || second.ClaimLost != 1 {
		t.Errorf("second run = %+v, want it to lose the claim", second)
	}
	if len(w.mailer.sent) != 1 {
		t.Errorf("the user was emailed %d times, want exactly once", len(w.mailer.sent))
	}
}

func TestRunClaimsAndRecordsBeforeSending(t *testing.T) {
	w := newWorld(t)
	w.mailer.onSend = func() {
		if s := w.schedule(); s.LastSentAt == nil || s.NextScheduledAt == nil {
			t.Error("the email went out before the schedule was claimed")
		}
		if len(w.store.Deliveries(w.user.ID)) != 1 {
			t.Error("the email went out before its delivery was recorded")
		}
	}
	if sum := w.run(w.runner(ModeSend, w.user.Email)); sum.Sent != 1 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestRunDoesNotEmailAUserWhoJustOptedOut(t *testing.T) {
	w := newWorld(t)
	due, _ := w.store.GetUsersForDigest(context.Background(), now)

	optedOut := w.store.User(w.user.ID)
	optedOut.Preferences.Digest.Enabled = false
	w.store.AddUser(optedOut)

	sum := w.run(w.runnerOn(&staleReads{Store: w.store, due: due}, ModeSend, w.user.Email))

	if sum.Sent != 0 || sum.ClaimLost != 1 || len(w.mailer.sent) != 0 {
		t.Errorf("summary = %+v, emails = %d; want no email after opting out", sum, len(w.mailer.sent))
	}
}

func TestRunReleasesTheClaimWhenTheSendDefinitelyFailed(t *testing.T) {
	w := newWorld(t)
	w.mailer.err = &notifications.RejectedError{StatusCode: 422, ErrorCode: 300}
	before := w.schedule()

	sum := w.run(w.runner(ModeSend, w.user.Email))

	if sum.SendFailed != 1 || sum.Sent != 0 {
		t.Errorf("summary = %+v", sum)
	}
	after := w.schedule()
	if !releasedAndDue(after) {
		t.Errorf("schedule = %+v, want the claim released", after)
	}
	// An overlapping run that read the schedule before this one claimed it
	// selected its events around this run's delivery records. It must not be
	// able to claim with what it read.
	won, err := w.store.CompareAndSwapDigestSchedule(context.Background(), w.user.ID, before, after, true)
	if err != nil || won {
		t.Errorf("claim with the pre-release schedule = %v, %v; want it lost", won, err)
	}
	if got := w.store.Deliveries(w.user.ID); len(got) != 0 {
		t.Errorf("delivery records = %v, want none: nothing was delivered", got)
	}

	// With Postmark accepting mail again, the next run delivers.
	w.mailer.err = nil
	if sum := w.run(w.runner(ModeSend, w.user.Email)); sum.Sent != 1 || len(w.mailer.sent) != 1 {
		t.Errorf("retry summary = %+v, emails = %d", sum, len(w.mailer.sent))
	}
	if got := w.store.Deliveries(w.user.ID); len(got) != 1 {
		t.Errorf("delivery records after the retry = %v, want the one event", got)
	}
}

func TestRunKeepsTheClaimWhenTheSendOutcomeIsUnknown(t *testing.T) {
	w := newWorld(t)
	w.mailer.err = errors.New("net/http: timeout awaiting response headers")

	sum := w.run(w.runner(ModeSend, w.user.Email))

	if sum.SendUncertain != 1 || sum.SendFailed != 0 || sum.Sent != 0 {
		t.Errorf("summary = %+v", sum)
	}
	if s := w.schedule(); s.LastSentAt == nil || s.NextScheduledAt == nil {
		t.Errorf("schedule = %+v, want the claim kept", s)
	}
	if got := w.store.Deliveries(w.user.ID); len(got) != 1 {
		t.Errorf("delivery records = %v, want the event kept as possibly delivered", got)
	}

	// The email may have gone out, so the next run must not try again.
	w.mailer.err = nil
	if sum := w.run(w.runner(ModeSend, w.user.Email)); sum.Sent != 0 || len(w.mailer.sent) != 0 {
		t.Errorf("retry summary = %+v, emails = %d; want no second attempt", sum, len(w.mailer.sent))
	}

	// Nor may the digest after that one, whose window reaches back over it.
	next := w.runner(ModeSend, w.user.Email)
	next.now = func() time.Time { return day(7) }
	if sum := w.run(next); sum.Due != 1 || sum.NothingNew != 1 || len(w.mailer.sent) != 0 {
		t.Errorf("next digest: summary = %+v, emails = %d; want nothing repeated", sum, len(w.mailer.sent))
	}
}

func TestRunSkipsUsersWithoutConsent(t *testing.T) {
	w := newWorld(t)
	due, _ := w.store.GetUsersForDigest(context.Background(), now)
	due[0].Preferences.DigestConsent = false // as if the query had let one through

	sum := w.run(w.runnerOn(&staleReads{Store: w.store, due: due}, ModeSend, w.user.Email))
	if sum.Sent != 0 || len(w.mailer.sent) != 0 {
		t.Errorf("summary = %+v; a user without consent was emailed", sum)
	}
}

func TestRunLogsNoAddressesOrContent(t *testing.T) {
	w := newWorld(t)
	w.run(w.runner(ModeDryRun, ""))
	w.run(w.runner(ModeSend, ""))
	w.run(w.runner(ModeSend, w.user.Email))

	logs := w.logs.String()
	if logs == "" {
		t.Fatal("nothing was logged")
	}
	for _, secret := range []string{"viewer@example.com", "@", "The Matrix", "<html"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain %q:\n%s", secret, logs)
		}
	}
	if !strings.Contains(logs, w.user.ID.Hex()) {
		t.Error("logs do not identify the user by ID")
	}
}

func TestRunFailures(t *testing.T) {
	w := newWorld(t)
	r := w.runner(ModeSend, w.user.Email)

	w.store.Err = errors.New("database down")
	if _, err := r.Run(context.Background()); err == nil {
		t.Error("Run succeeded although the due users could not be listed")
	}
	w.store.Err = nil

	// Every due user failing is a failed run; the claim was never taken.
	broken := &brokenEvents{Store: w.store}
	sum, err := w.runnerOn(broken, ModeSend, w.user.Email).Run(context.Background())
	if err == nil || sum.Errors != 1 {
		t.Errorf("Run = %+v, %v; want an error when every user failed", sum, err)
	}
	if len(w.mailer.sent) != 0 {
		t.Error("an email was sent for a user whose data could not be loaded")
	}
}

type brokenEvents struct{ *storetest.Store }

func (*brokenEvents) ListAvailabilityEvents(context.Context, []string, string, time.Time) ([]models.AvailabilityEvent, error) {
	return nil, errors.New("cursor timeout")
}

func TestGateRefusesEveryoneItShould(t *testing.T) {
	user := subscriber(models.ServiceNetflix)
	email := Email{Subject: "s", HTML: "h"}
	allow := ParseAllowlist(user.Email)

	tests := []struct {
		name string
		gate func(m Mailer) gate
	}{
		{"dry-run mode", func(m Mailer) gate { return gate{mode: ModeDryRun, allow: allow, mailer: m} }},
		{"not on the allowlist", func(m Mailer) gate {
			return gate{mode: ModeSend, allow: ParseAllowlist("other@example.com"), mailer: m}
		}},
		{"nil allowlist", func(m Mailer) gate { return gate{mode: ModeSend, mailer: m} }},
		{"zero-value mode", func(m Mailer) gate { return gate{allow: allow, mailer: m} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mailer := &fakeMailer{}
			if err := tt.gate(mailer).send(user, email); !errors.Is(err, errNotPermitted) {
				t.Errorf("send() error = %v, want errNotPermitted", err)
			}
			if len(mailer.sent) != 0 {
				t.Error("the gate let an email through")
			}
		})
	}

	mailer := &fakeMailer{}
	if err := (gate{mode: ModeSend, allow: allow, mailer: mailer}).send(user, email); err != nil || len(mailer.sent) != 1 {
		t.Errorf("an allowlisted send in send mode failed: %v", err)
	}
}

func TestGateChecksTheAddressItDeliversTo(t *testing.T) {
	email := Email{Subject: "s", HTML: "h"}
	const account = "viewer@example.com"

	tests := []struct {
		name            string
		notifyEmail     string
		useAccountEmail bool
		allowlist       string
		wantTo          string // empty: refused
	}{
		{"account email", "", true, account, account},
		{"notification address not listed", "stranger@example.net", false, account, ""},
		{"notification address listed, account not", "alerts@example.com", false, "alerts@example.com", ""},
		{"both listed", "alerts@example.com", false, account + ",alerts@example.com", "alerts@example.com"},
		{"notification address is the account email", " Viewer@Example.com ", false, account, "Viewer@Example.com"},
		{"notification address set but not in use", "stranger@example.net", true, account, account},
		{"blank notification address falls back to the account", "  ", false, account, account},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := subscriber(models.ServiceNetflix)
			user.Preferences.NotifyEmail = tt.notifyEmail
			user.Preferences.UseAccountEmail = tt.useAccountEmail
			mailer := &fakeMailer{}
			g := gate{mode: ModeSend, allow: ParseAllowlist(tt.allowlist), mailer: mailer}

			err := g.send(user, email)

			if tt.wantTo == "" {
				if !errors.Is(err, errNotPermitted) || len(mailer.sent) != 0 || g.permits(user) {
					t.Errorf("send() = %v with %d emails, permits = %v; want it refused", err, len(mailer.sent), g.permits(user))
				}
				return
			}
			if err != nil || len(mailer.sent) != 1 || mailer.sent[0].to != tt.wantTo || !g.permits(user) {
				t.Errorf("send() = %v, sent = %+v; want one email to %q", err, mailer.sent, tt.wantTo)
			}
		})
	}
}

func TestNewRunnerValidation(t *testing.T) {
	store := storetest.New()
	if _, err := NewRunner(store, nil, Config{Mode: ModeSend}, nil); err == nil {
		t.Error("send mode without a mailer was accepted")
	}
	if _, err := NewRunner(store, nil, Config{Mode: "yolo"}, nil); err == nil {
		t.Error("an unknown mode was accepted")
	}
	if _, err := NewRunner(store, nil, Config{}, nil); err != nil {
		t.Errorf("dry run without a mailer: %v", err)
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"", ModeDryRun, false},
		{"dry-run", ModeDryRun, false},
		{" DRY-RUN ", ModeDryRun, false},
		{"send", ModeSend, false},
		{"Send", ModeSend, false},
		{"true", "", true},
		{"live", "", true},
	}
	for _, tt := range tests {
		got, err := ParseMode(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("ParseMode(%q) = (%q, %v), want (%q, error %v)", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestAllowlist(t *testing.T) {
	allow := ParseAllowlist(" A@Example.com,, b@example.com ,")
	if len(allow) != 2 {
		t.Errorf("ParseAllowlist kept %d entries, want 2", len(allow))
	}
	for email, want := range map[string]bool{
		"a@example.com":   true,
		" B@EXAMPLE.COM ": true,
		"c@example.com":   false,
		"":                false,
	} {
		if got := allow.Allows(email); got != want {
			t.Errorf("Allows(%q) = %v, want %v", email, got, want)
		}
	}
	if (Allowlist)(nil).Allows("a@example.com") || ParseAllowlist("").Allows("") {
		t.Error("an empty allowlist allowed someone")
	}
}
