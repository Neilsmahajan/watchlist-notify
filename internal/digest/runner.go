package digest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/notifications"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Mode says whether the digest job may send email.
type Mode string

const (
	// ModeDryRun works out who would be emailed about what, logs counts, and
	// writes nothing. It is the default.
	ModeDryRun Mode = "dry-run"
	// ModeSend emails allowlisted users and advances their schedule.
	ModeSend Mode = "send"
)

// ParseMode reads a DIGEST_MODE value. Empty means dry-run; anything other
// than the two known modes is an error rather than a guess.
func ParseMode(value string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "dry-run", "dryrun", "dry_run":
		return ModeDryRun, nil
	case "send":
		return ModeSend, nil
	default:
		return "", fmt.Errorf("unknown digest mode %q (want %q or %q)", value, ModeDryRun, ModeSend)
	}
}

// Allowlist is the set of account emails the digest job may email. The zero
// value allows no one.
type Allowlist map[string]struct{}

// ParseAllowlist reads a comma-separated DIGEST_ALLOWLIST value.
func ParseAllowlist(value string) Allowlist {
	allow := Allowlist{}
	for _, part := range strings.Split(value, ",") {
		if email := normalizeEmail(part); email != "" {
			allow[email] = struct{}{}
		}
	}
	return allow
}

// Allows reports whether the account with this email may be emailed.
func (a Allowlist) Allows(accountEmail string) bool {
	email := normalizeEmail(accountEmail)
	if email == "" {
		return false
	}
	_, ok := a[email]
	return ok
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// Mailer sends one HTML email.
type Mailer interface {
	SendEmail(toEmail, subject, htmlBody string) error
}

// Store is the persistence the digest job needs.
type Store interface {
	GetUsersForDigest(ctx context.Context, now time.Time) ([]*models.User, error)
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
	ListAvailabilityEvents(ctx context.Context, titleKeys []string, region string, since time.Time) ([]models.AvailabilityEvent, error)
	// CompareAndSwapDigestSchedule moves a user's digest schedule from one
	// state to another and reports whether the stored state still matched.
	CompareAndSwapDigestSchedule(ctx context.Context, userID primitive.ObjectID, from, to models.DigestSchedule, requireEnabled bool) (bool, error)
}

// Config controls a Runner.
type Config struct {
	Mode      Mode
	Allowlist Allowlist
	// SiteURL is the web app's public address, used for links in the email.
	SiteURL string
	// DefaultRegion is used for users without a valid region.
	DefaultRegion string
}

// errNotPermitted is returned by the gate when asked to email someone the
// configuration does not allow. Reaching it means a bug in the runner.
var errNotPermitted = errors.New("digest: recipient is not permitted by mode and allowlist")

// gate is the only way from the runner to the mailer. It re-checks the mode
// and the allowlist on every send, so a mistake in the runner's own control
// flow still cannot email someone who is not allowlisted.
type gate struct {
	mode   Mode
	allow  Allowlist
	mailer Mailer
}

func (g gate) permits(user *models.User) bool {
	return g.mode == ModeSend && g.mailer != nil && g.allow.Allows(user.Email)
}

func (g gate) send(user *models.User, email Email) error {
	if !g.permits(user) {
		return errNotPermitted
	}
	return g.mailer.SendEmail(user.DigestRecipient(), email.Subject, email.HTML)
}

// Runner sends digests to the users who are due.
type Runner struct {
	store  Store
	gate   gate
	cfg    Config
	logger *slog.Logger
	now    func() time.Time
}

// NewRunner returns a Runner. mailer may be nil in dry-run mode, which never
// sends; send mode requires one.
func NewRunner(store Store, mailer Mailer, cfg Config, logger *slog.Logger) (*Runner, error) {
	switch cfg.Mode {
	case "":
		cfg.Mode = ModeDryRun
	case ModeDryRun, ModeSend:
	default:
		return nil, fmt.Errorf("unknown digest mode %q", cfg.Mode)
	}
	if cfg.Mode == ModeSend && mailer == nil {
		return nil, errors.New("digest: send mode needs a mailer")
	}
	if strings.TrimSpace(cfg.DefaultRegion) == "" {
		cfg.DefaultRegion = models.DefaultRegion
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		store:  store,
		gate:   gate{mode: cfg.Mode, allow: cfg.Allowlist, mailer: mailer},
		cfg:    cfg,
		logger: logger,
		now:    time.Now,
	}, nil
}

// Summary is the outcome of a digest run. Every due user lands in exactly one
// of the counters below Due.
type Summary struct {
	Mode Mode
	// Due is how many users were due for a digest.
	Due int

	// NothingNew: nothing on their list newly became available. They stay due.
	NothingNew int
	// DryRun: would have been emailed, but the job ran in dry-run mode.
	DryRun int
	// NotAllowlisted: would have been emailed, but their account is not on
	// the allowlist.
	NotAllowlisted int
	// Sent: emailed, schedule advanced.
	Sent int
	// ClaimLost: another run claimed the user first; that run sends.
	ClaimLost int
	// SendFailed: the send definitely failed; the claim was released so the
	// next run retries.
	SendFailed int
	// SendUncertain: the send may or may not have gone out; the claim is
	// kept so the user is not emailed twice.
	SendUncertain int
	// Errors: the user's data could not be loaded or rendered.
	Errors int

	// Titles is the number of titles across everyone who had something to
	// send, whether or not it was sent.
	Titles   int
	Duration time.Duration
}

// Recipients is how many users had something to be emailed about.
func (s Summary) Recipients() int {
	return s.DryRun + s.NotAllowlisted + s.Sent + s.ClaimLost + s.SendFailed + s.SendUncertain
}

type outcome int

const (
	outcomeNothingNew outcome = iota
	outcomeDryRun
	outcomeNotAllowlisted
	outcomeSent
	outcomeClaimLost
	outcomeSendFailed
	outcomeSendUncertain
)

func (o outcome) String() string {
	switch o {
	case outcomeNothingNew:
		return "nothing_new"
	case outcomeDryRun:
		return "dry_run"
	case outcomeNotAllowlisted:
		return "not_allowlisted"
	case outcomeSent:
		return "sent"
	case outcomeClaimLost:
		return "claim_lost"
	case outcomeSendFailed:
		return "send_failed"
	case outcomeSendUncertain:
		return "send_uncertain"
	default:
		return "unknown"
	}
}

// Run processes every due user once. One user's failure does not stop the
// others; Run returns an error only when the due users could not be listed or
// when every one of them failed.
//
// Logs identify users by ID, never by email address, and never include the
// email body.
func (r *Runner) Run(ctx context.Context) (Summary, error) {
	start := r.now()
	now := start.UTC().Truncate(time.Millisecond)
	sum := Summary{Mode: r.cfg.Mode}

	users, err := r.store.GetUsersForDigest(ctx, now)
	if err != nil {
		return sum, fmt.Errorf("list users due for a digest: %w", err)
	}
	sum.Due = len(users)

	for _, user := range users {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		result, titles, err := r.process(ctx, user, now)
		if err != nil {
			sum.Errors++
			r.logger.Error("digest: user failed", "user_id", user.ID.Hex(), "error", err.Error())
			continue
		}
		switch result {
		case outcomeNothingNew:
			sum.NothingNew++
		case outcomeDryRun:
			sum.DryRun++
		case outcomeNotAllowlisted:
			sum.NotAllowlisted++
		case outcomeSent:
			sum.Sent++
		case outcomeClaimLost:
			sum.ClaimLost++
		case outcomeSendFailed:
			sum.SendFailed++
		case outcomeSendUncertain:
			sum.SendUncertain++
		}
		sum.Titles += titles
		if result != outcomeNothingNew {
			r.logger.Info("digest: user processed",
				"user_id", user.ID.Hex(), "outcome", result.String(), "titles", titles)
		}
	}
	sum.Duration = r.now().Sub(start)

	if sum.Due > 0 && sum.Errors == sum.Due {
		return sum, fmt.Errorf("digest: all %d due users failed", sum.Due)
	}
	return sum, nil
}

// process handles one due user and reports what happened and how many titles
// their digest had.
func (r *Runner) process(ctx context.Context, user *models.User, now time.Time) (outcome, int, error) {
	settings := user.Preferences.Digest
	if !settings.Enabled || !user.Preferences.DigestConsent {
		// The due query already filters on both; never rely on that alone
		// for something as visible as an email.
		return outcomeNothingNew, 0, nil
	}

	entries, err := r.entries(ctx, user, now)
	if err != nil {
		return 0, 0, err
	}
	if len(entries) == 0 {
		// Nothing to say: send nothing and leave the schedule alone, so the
		// user is looked at again on the next run and hears about a new
		// arrival within a day instead of an interval later.
		return outcomeNothingNew, 0, nil
	}
	if r.cfg.Mode != ModeSend {
		return outcomeDryRun, len(entries), nil
	}
	if !r.gate.permits(user) {
		return outcomeNotAllowlisted, len(entries), nil
	}

	// Render before claiming: a template failure must not cost the user
	// their digest.
	email, err := Render(r.cfg.SiteURL, entries)
	if err != nil {
		return 0, 0, err
	}

	// Claim the user by moving their schedule forward, conditional on it
	// still being what this run read. Two overlapping runs both see the user
	// as due; only one of them wins this write, and only the winner sends.
	sentAt := now
	next := NextSendTime(settings.Interval, settings.IntervalUnit, &sentAt, now)
	unclaimed := settings.Schedule()
	claimed := models.DigestSchedule{LastSentAt: &sentAt, NextScheduledAt: &next}
	won, err := r.store.CompareAndSwapDigestSchedule(ctx, user.ID, unclaimed, claimed, true)
	if err != nil {
		return 0, 0, fmt.Errorf("claim digest: %w", err)
	}
	if !won {
		return outcomeClaimLost, len(entries), nil
	}

	if err := r.gate.send(user, email); err != nil {
		if !notifications.IsDefiniteFailure(err) {
			// The request may have reached Postmark. Keeping the claim means
			// this user might miss one digest; releasing it could send the
			// same digest twice. Missing one is the lesser harm.
			r.logger.Error("digest: send outcome unknown, claim kept",
				"user_id", user.ID.Hex(), "error", err.Error())
			return outcomeSendUncertain, len(entries), nil
		}
		// Nothing went out. Put the schedule back so the next run retries.
		// A detached context: the release should still happen if the run is
		// being cancelled.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		released, releaseErr := r.store.CompareAndSwapDigestSchedule(releaseCtx, user.ID, claimed, unclaimed, false)
		r.logger.Error("digest: send failed",
			"user_id", user.ID.Hex(), "error", err.Error(), "claim_released", released && releaseErr == nil)
		return outcomeSendFailed, len(entries), nil
	}
	return outcomeSent, len(entries), nil
}

// entries loads what Select needs for one user and applies it.
func (r *Runner) entries(ctx context.Context, user *models.User, now time.Time) ([]Entry, error) {
	if len(user.ActiveServiceCodes()) == 0 {
		return nil, nil
	}
	items, err := r.store.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user.ID})
	if err != nil {
		return nil, fmt.Errorf("load watchlist: %w", err)
	}

	var keys []string
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		ref, ok := item.TitleRef()
		if !ok || seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		keys = append(keys, ref.Key())
	}
	if len(keys) == 0 {
		return nil, nil
	}

	region := user.RegionOr(r.cfg.DefaultRegion)
	windowStart := WindowStart(user.Preferences.Digest, now)

	events, err := r.store.ListAvailabilityEvents(ctx, keys, region, windowStart.Add(-ReappearanceQuietPeriod))
	if err != nil {
		return nil, fmt.Errorf("load availability events: %w", err)
	}
	if len(events) == 0 {
		return nil, nil
	}
	titles, err := r.store.GetTitles(ctx, keys, []string{region})
	if err != nil {
		return nil, fmt.Errorf("load titles: %w", err)
	}

	return Select(Selection{
		User:        user,
		Region:      region,
		Items:       items,
		Titles:      titles,
		Events:      events,
		WindowStart: windowStart,
		Now:         now,
	}), nil
}
