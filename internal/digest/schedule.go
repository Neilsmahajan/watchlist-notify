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

// DeliveryOverlap is how far behind the last digest the next one looks again.
//
// A digest cannot simply pick up where the previous one stopped. An event is
// stamped with the time the change was detected, but it becomes readable a
// little later, when the write commits; and it becomes true later still if
// the snapshot write behind it had to be retried on the next run. Either way
// an event can turn up, or start to qualify, carrying a detection time that a
// digest has already moved past. Reading the last DeliveryOverlap again
// catches those. The delivery log (models.DigestDelivery) keeps the re-read
// from announcing anything twice.
const DeliveryOverlap = 7 * 24 * time.Hour

// WindowStart is the earliest detection time a digest sent at now considers:
// DeliveryOverlap before the last digest, or one interval back for a first
// digest.
func WindowStart(settings models.DigestSettings, now time.Time) time.Time {
	if settings.LastSentAt != nil && !settings.LastSentAt.IsZero() {
		return settings.LastSentAt.Add(-DeliveryOverlap)
	}
	return AddInterval(now, settings.Interval, settings.IntervalUnit, -1)
}
