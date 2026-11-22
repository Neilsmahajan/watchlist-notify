package models

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ServiceSubscription struct {
	Code    string    `bson:"code" json:"code"` // e.g. "netflix"
	AddedAt time.Time `bson:"added_at" json:"added_at"`
	Active  bool      `bson:"active" json:"active"`
	Plan    string    `bson:"plan,omitempty" json:"plan,omitempty"` // optional
}

type DigestSettings struct {
	Enabled         bool       `bson:"enabled" json:"enabled"`
	Interval        int        `bson:"interval" json:"interval"`           // >=1
	IntervalUnit    string     `bson:"interval_unit" json:"interval_unit"` // e.g. "days", "weeks", "months"
	LastSentAt      *time.Time `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	NextScheduledAt *time.Time `bson:"next_scheduled_at,omitempty" json:"next_scheduled_at,omitempty"`
}

// DigestSchedule is the part of DigestSettings that tracks sends. The digest
// job claims a user by compare-and-swapping these two fields together.
type DigestSchedule struct {
	LastSentAt      *time.Time
	NextScheduledAt *time.Time
}

// Schedule returns the settings' current schedule state.
func (d DigestSettings) Schedule() DigestSchedule {
	return DigestSchedule{LastSentAt: d.LastSentAt, NextScheduledAt: d.NextScheduledAt}
}

type Preferences struct {
	NotifyEmail      string         `bson:"notify_email,omitempty" json:"notify_email,omitempty"`
	UseAccountEmail  bool           `bson:"use_account_email" json:"use_account_email"`
	MarketingConsent bool           `bson:"marketing_consent" json:"marketing_consent"`
	DigestConsent    bool           `bson:"digest_consent" json:"digest_consent"`
	Digest           DigestSettings `bson:"digest" json:"digest"`
}

type User struct {
	ID          primitive.ObjectID    `bson:"_id,omitempty" json:"id"`
	Email       string                `bson:"email" json:"email"`
	Name        string                `bson:"name" json:"name"`
	Picture     string                `bson:"picture" json:"picture"`
	Region      string                `bson:"region" json:"region"`
	Services    []ServiceSubscription `bson:"services,omitempty" json:"services,omitempty"`
	Preferences Preferences           `bson:"preferences" json:"preferences"`
	// IMDbSync is present while the user has a public IMDb list linked.
	IMDbSync *IMDbSync `bson:"imdb_sync,omitempty" json:"imdb_sync,omitempty"`
	// IMDbAttemptAt is when a fetch of the user's IMDb list was last started.
	// It throttles fetches (one a day from the worker, one a minute from the
	// link endpoint) and lives outside IMDbSync so that unlinking and linking
	// again cannot reset it.
	IMDbAttemptAt *time.Time `bson:"imdb_attempt_at,omitempty" json:"-"`
	CreatedAt     time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `bson:"updated_at" json:"updated_at"`
}

// Outcomes of an IMDb sync run, stored in IMDbSync.LastStatus.
const (
	// IMDbStatusOK: the whole list was read and every change was applied.
	IMDbStatusOK = "ok"
	// IMDbStatusPartial: some of the list was read or applied; the next run
	// continues. A partial run never removes anything.
	IMDbStatusPartial = "partial"
	// IMDbStatusFailed: IMDb could not be read at all.
	IMDbStatusFailed = "failed"
	// IMDbStatusNotFound: the list does not exist or is not public.
	IMDbStatusNotFound = "not_found"
)

// IMDbSync is a user's link to a public IMDb list and the outcome of the
// last sync. Only the list's ID is kept; nothing else from IMDb is stored.
type IMDbSync struct {
	// SourceID is an IMDb user ID (ur…, meaning that user's Watchlist) or a
	// list ID (ls…).
	SourceID string    `bson:"source_id" json:"source_id"`
	LinkedAt time.Time `bson:"linked_at" json:"linked_at"`
	// LastAttemptAt is when the list was last fetched, successfully or not.
	LastAttemptAt *time.Time `bson:"last_attempt_at,omitempty" json:"last_attempt_at,omitempty"`
	// LastSyncedAt is when the list was last read completely.
	LastSyncedAt *time.Time `bson:"last_synced_at,omitempty" json:"last_synced_at,omitempty"`
	LastStatus   string     `bson:"last_status,omitempty" json:"last_status,omitempty"` // ok | partial | failed | not_found
	LastError    string     `bson:"last_error,omitempty" json:"last_error,omitempty"`
	// Total is the number of entries IMDb reported for the list.
	Total int `bson:"total" json:"total"`
	// Added, Removed and Unmatched are counts from the last run. Unmatched
	// are IMDb titles TMDb has no movie or show for (episodes, games, …).
	Added     int `bson:"added" json:"added"`
	Removed   int `bson:"removed" json:"removed"`
	Unmatched int `bson:"unmatched" json:"unmatched"`
}

// ActiveServiceCodes returns the codes of the services the user currently has.
func (u *User) ActiveServiceCodes() map[string]bool {
	active := make(map[string]bool, len(u.Services))
	for _, svc := range u.Services {
		if svc.Active {
			active[strings.ToLower(svc.Code)] = true
		}
	}
	return active
}

// RegionOr returns the user's region, or fallback when it is unset or invalid.
func (u *User) RegionOr(fallback string) string {
	if region, ok := NormalizeRegion(u.Region); ok {
		return region
	}
	return fallback
}

// DigestRecipient returns the address digests go to: the notification email
// when the user set one and opted out of using the account email.
func (u *User) DigestRecipient() string {
	if !u.Preferences.UseAccountEmail && strings.TrimSpace(u.Preferences.NotifyEmail) != "" {
		return strings.TrimSpace(u.Preferences.NotifyEmail)
	}
	return u.Email
}
