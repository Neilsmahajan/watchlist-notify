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
	CreatedAt   time.Time             `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time             `bson:"updated_at" json:"updated_at"`
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
