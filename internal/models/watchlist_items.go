package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ExternalIDs struct {
	TMDB int    `bson:"tmdb,omitempty" json:"tmdb,omitempty"`
	IMDB string `bson:"imdb,omitempty" json:"imdb,omitempty"`
	TVDB int    `bson:"tvdb,omitempty" json:"tvdb,omitempty"`
}

type AvailabilityMatch struct {
	ServiceCode string    `bson:"service_code" json:"service_code"`
	Region      string    `bson:"region" json:"region"`
	Available   bool      `bson:"available" json:"available"`
	CheckedAt   time.Time `bson:"checked_at" json:"checked_at"`
	URL         string    `bson:"url,omitempty" json:"url,omitempty"`
}

type WatchlistItem struct {
	ID            primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	UserID        primitive.ObjectID  `bson:"user_id" json:"user_id"`
	Title         string              `bson:"title" json:"title"`
	Type          string              `bson:"type" json:"type"` // enum: movie | show (See constants)
	Year          int                 `bson:"year,omitempty" json:"year,omitempty"`
	ExternalIDs   ExternalIDs         `bson:"external_ids,omitempty" json:"external_ids,omitempty"`
	Tags          []string            `bson:"tags,omitempty" json:"tags,omitempty"`
	Status        string              `bson:"status" json:"status"` // enum: planned | watching | finished
	AddedAt       time.Time           `bson:"added_at" json:"added_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
	LastCheckedAt *time.Time          `bson:"last_checked_at,omitempty" json:"last_checked_at,omitempty"`
	Availability  []AvailabilityMatch `bson:"availability,omitempty" json:"availability,omitempty"` // optional embeded or compute
}
