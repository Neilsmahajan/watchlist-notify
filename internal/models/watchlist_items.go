package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
	TMDbID        int                 `bson:"tmdb_id,omitempty" json:"tmdb_id,omitempty"`
	Tags          []string            `bson:"tags,omitempty" json:"tags,omitempty"`
	Status        string              `bson:"status" json:"status"` // enum: planned | watching | finished
	AddedAt       time.Time           `bson:"added_at" json:"added_at"`
	UpdatedAt     time.Time           `bson:"updated_at" json:"updated_at"`
	LastCheckedAt *time.Time          `bson:"last_checked_at,omitempty" json:"last_checked_at,omitempty"`
	Availability  []AvailabilityMatch `bson:"availability,omitempty" json:"availability,omitempty"` // optional embeded or compute
}

type ListWatchlistOptions struct {
	UserID primitive.ObjectID
	Limit  int
	Offset int
	Status string
	Type   string
	Search string
	Sort   string
}
