package models

import (
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
	IntervalValue   int        `bson:"interval_value" json:"interval_value"` // >=1
	IntervalUnit    string     `bson:"interval_unit" json:"interval_unit"`   // e.g. "day", "week", "month"
	AnchorDate      time.Time  `bson:"anchor_date" json:"anchor_date"`       // first send, UTC midnight
	PreferredHour   int        `bson:"preferred_hour" json:"preferred_hour"` // 0-23, default to 14 (2 PM)
	Timezone        string     `bson:"timezone" json:"timezone"`             // IANA timezone name, e.g. "America/New_York"
	LastSentAt      *time.Time `bson:"last_sent_at,omitempty" json:"last_sent_at,omitempty"`
	NextScheduledAt *time.Time `bson:"next_scheduled_at,omitempty" json:"next_scheduled_at,omitempty"`
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
