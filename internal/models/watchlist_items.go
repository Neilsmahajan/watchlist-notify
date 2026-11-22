package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type WatchlistItem struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    primitive.ObjectID `bson:"user_id" json:"user_id"`
	Title     string             `bson:"title" json:"title"`
	Type      string             `bson:"type" json:"type"` // enum: movie | show (See constants)
	Year      int                `bson:"year,omitempty" json:"year,omitempty"`
	TMDbID    int                `bson:"tmdb_id,omitempty" json:"tmdb_id,omitempty"`
	IMDbID    string             `bson:"imdb_id,omitempty" json:"imdb_id,omitempty"`
	Tags      []string           `bson:"tags,omitempty" json:"tags,omitempty"`
	Status    string             `bson:"status" json:"status"` // enum: planned | watching | finished
	AddedAt   time.Time          `bson:"added_at" json:"added_at"`
	UpdatedAt time.Time          `bson:"updated_at" json:"updated_at"`
}

// TitleRef returns the shared title this item points at. ok is false for
// items that were never linked to a TMDb entry.
func (w *WatchlistItem) TitleRef() (ref TitleRef, ok bool) {
	if w.TMDbID <= 0 {
		return TitleRef{}, false
	}
	mediaType, ok := TMDbMediaType(w.Type)
	if !ok {
		return TitleRef{}, false
	}
	return TitleRef{MediaType: mediaType, TMDbID: w.TMDbID}, true
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
