package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Where a watchlist item came from. Items added by hand, and every item that
// predates the field, have no source.
const (
	// SourceIMDb marks an item the IMDb sync added, or adopted because it
	// came from the user's IMDb list in the first place (see Adoptable).
	// Only these items can be removed by the sync, and only while the user
	// has not touched them.
	SourceIMDb = "imdb"
	// SourceCSV marks an item created by a CSV import.
	SourceCSV = "csv"
)

type WatchlistItem struct {
	ID     primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID primitive.ObjectID `bson:"user_id" json:"user_id"`
	Title  string             `bson:"title" json:"title"`
	Type   string             `bson:"type" json:"type"` // enum: movie | show (See constants)
	Year   int                `bson:"year,omitempty" json:"year,omitempty"`
	TMDbID int                `bson:"tmdb_id,omitempty" json:"tmdb_id,omitempty"`
	IMDbID string             `bson:"imdb_id,omitempty" json:"imdb_id,omitempty"`
	Tags   []string           `bson:"tags,omitempty" json:"tags,omitempty"`
	Status string             `bson:"status" json:"status"` // enum: planned | watching | finished
	// Source is SourceIMDb, SourceCSV, or empty for items added by hand.
	Source string `bson:"source,omitempty" json:"source,omitempty"`
	// IMDbMissingSince is set by the IMDb sync when a complete fetch of the
	// user's linked list no longer contains this item. It is the first of the
	// two strikes the sync needs before it removes an item; seeing the title
	// on the list again clears it.
	IMDbMissingSince *time.Time `bson:"imdb_missing_since,omitempty" json:"imdb_missing_since,omitempty"`
	AddedAt          time.Time  `bson:"added_at" json:"added_at"`
	UpdatedAt        time.Time  `bson:"updated_at" json:"updated_at"`
}

// SyncOwned reports whether the IMDb sync may still remove this item: the
// sync added it or took it over (see Adoptable), and the user has neither
// changed its status nor tagged it.
// Anything else belongs to the user and is never removed automatically.
func (w *WatchlistItem) SyncOwned() bool {
	return w.Source == SourceIMDb && w.Status == WatchlistStatusPlanned && len(w.Tags) == 0 && w.IMDbID != ""
}

// Adoptable reports whether the IMDb sync may take this item over when it
// finds the item's own IMDb ID on the linked list: an item that carries an
// IMDb ID, was not added by the sync, and that the user has not touched
// (still "planned", no tags).
//
// Only an import ever stores an IMDb ID on an item: one added by hand has
// none (the create request's imdb_id is ignored and an edit cannot set it),
// and the sync never writes one onto an existing item. So an adoptable item
// is one the user brought over from IMDb themselves, before they linked the
// list, and it is safe to treat it like the titles the sync adds.
func (w *WatchlistItem) Adoptable() bool {
	return w.IMDbID != "" && (w.Source == "" || w.Source == SourceCSV) &&
		w.Status == WatchlistStatusPlanned && len(w.Tags) == 0
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
