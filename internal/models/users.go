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

type Preferences struct {
	NotifyEmail      string     `bson:"notify_email,omitempty" json:"notify_email,omitempty"`
	UseAccountEmail  bool       `bson:"use_account_email" json:"use_account_email"`
	MarketingConsent bool       `bson:"marketing_consent" json:"marketing_consent"`
	DigestConsent    bool       `bson:"digest_consent" json:"digest_consent"`
	DigestFrequency  string     `bson:"digest_frequency" json:"digest_frequency"`                       // enum: daily | weekly | manual
	QuietHoursStart  int        `bson:"quiet_hours_start,omitempty" json:"quiet_hours_start,omitempty"` // 0-23
	QuietHoursEnd    int        `bson:"quiet_hours_end,omitempty" json:"quiet_hours_end,omitempty"`     // 0-23
	LastDigestSentAt *time.Time `bson:"last_digest_sent_at,omitempty" json:"last_digest_sent_at,omitempty"`
}

type User struct {
	ID          primitive.ObjectID    `bson:"_id,omitempty" json:"id"`
	GoogleID    string                `bson:"google_id,omitempty" json:"google_id"`
	Email       string                `bson:"email" json:"email"`
	Name        string                `bson:"name" json:"name"`
	Picture     string                `bson:"picture" json:"picture"`
	Region      string                `bson:"region" json:"region"`
	Services    []ServiceSubscription `bson:"services,omitempty" json:"services,omitempty"`
	Preferences Preferences           `bson:"preferences" json:"preferences"`
	CreatedAt   time.Time             `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time             `bson:"updated_at" json:"updated_at"`
}
