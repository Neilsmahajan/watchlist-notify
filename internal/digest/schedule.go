// Package digest decides which watchlist titles a user should hear about,
// renders the email, and sends it at most once per interval.
package digest

import (
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// AddInterval moves t by a digest interval. Units are the ones stored on user
// documents ("days", "weeks", "months"); an unknown unit falls back to seven
// days and an interval below one is treated as one. A negative n goes back.
func AddInterval(t time.Time, interval int, unit string, n int) time.Time {
	if interval < 1 {
		interval = 1
	}
	interval *= n
	switch unit {
	case models.DigestUnitDays:
		return t.AddDate(0, 0, interval)
	case models.DigestUnitWeeks:
		return t.AddDate(0, 0, interval*7)
	case models.DigestUnitMonths:
		return t.AddDate(0, interval, 0)
	default:
		return t.AddDate(0, 0, 7*n)
	}
}

// NextSendTime is when the next digest is due: one interval after the last
// send, or after now when nothing was sent yet.
func NextSendTime(interval int, unit string, lastSent *time.Time, now time.Time) time.Time {
	base := now
	if lastSent != nil && !lastSent.IsZero() {
		base = *lastSent
	}
	return AddInterval(base, interval, unit, 1)
}

// WindowStart is the earliest detection time a digest sent at now covers:
// everything since the last digest, or one interval back for a first digest.
func WindowStart(settings models.DigestSettings, now time.Time) time.Time {
	if settings.LastSentAt != nil && !settings.LastSentAt.IsZero() {
		return *settings.LastSentAt
	}
	return AddInterval(now, settings.Interval, settings.IntervalUnit, -1)
}
