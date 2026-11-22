package digest

import (
	"testing"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

var now = time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)

func day(n int) time.Time { return now.AddDate(0, 0, n) }

func TestNextSendTime(t *testing.T) {
	lastSent := day(-3)
	tests := []struct {
		name     string
		interval int
		unit     string
		lastSent *time.Time
		want     time.Time
	}{
		{"days from now", 3, models.DigestUnitDays, nil, day(3)},
		{"weeks from now", 2, models.DigestUnitWeeks, nil, day(14)},
		{"months from now", 1, models.DigestUnitMonths, nil, now.AddDate(0, 1, 0)},
		{"counted from the last send", 7, models.DigestUnitDays, &lastSent, day(4)},
		{"zero last send counts from now", 1, models.DigestUnitDays, &time.Time{}, day(1)},
		{"unknown unit falls back to a week", 5, "fortnights", nil, day(7)},
		{"interval below one is one", 0, models.DigestUnitDays, nil, day(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextSendTime(tt.interval, tt.unit, tt.lastSent, now); !got.Equal(tt.want) {
				t.Errorf("NextSendTime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWindowStart(t *testing.T) {
	lastSent := day(-20)
	tests := []struct {
		name     string
		settings models.DigestSettings
		want     time.Time
	}{
		{"the last digest, less the overlap", models.DigestSettings{Interval: 1, IntervalUnit: models.DigestUnitWeeks, LastSentAt: &lastSent}, day(-27)},
		{"the overlap does not depend on the interval", models.DigestSettings{Interval: 6, IntervalUnit: models.DigestUnitMonths, LastSentAt: &lastSent}, day(-27)},
		{"first digest looks back one interval", models.DigestSettings{Interval: 1, IntervalUnit: models.DigestUnitWeeks}, day(-7)},
		{"first digest, daily", models.DigestSettings{Interval: 2, IntervalUnit: models.DigestUnitDays}, day(-2)},
		{"first digest, monthly", models.DigestSettings{Interval: 1, IntervalUnit: models.DigestUnitMonths}, now.AddDate(0, -1, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := WindowStart(tt.settings, now); !got.Equal(tt.want) {
				t.Errorf("WindowStart() = %v, want %v", got, tt.want)
			}
		})
	}
}

// An event has to still exist when the slowest digest comes round to it. The
// longest interval the preferences accept is 12 months, and the digest also
// looks back an overlap past the previous digest, and a quiet period before
// that to recognise reappearances.
func TestEventsOutliveTheLongestInterval(t *testing.T) {
	yearly := models.DigestSettings{Interval: 12, IntervalUnit: models.DigestUnitMonths}
	next := NextSendTime(yearly.Interval, yearly.IntervalUnit, &now, now)
	yearly.LastSentAt = &now

	oldestRead := WindowStart(yearly, next).Add(-ReappearanceQuietPeriod)
	if lookback := next.Sub(oldestRead); lookback >= models.EventRetention {
		t.Errorf("a yearly digest reads back %v but events are kept for only %v", lookback, models.EventRetention)
	}
}
