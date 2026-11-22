package database

import (
	"context"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// The writes the IMDb sync makes to single watchlist items. Each one is a
// conditional update or delete: the condition that makes the write safe is in
// the filter itself, so it is checked by MongoDB at the moment of the write
// and not only by the code that planned it. A user editing an item while the
// sync runs therefore always wins. Every filter is scoped by user_id.
//
// None of these writes touches updated_at. That field belongs to the user's
// own writes (creating and editing an item), which is what lets the delete
// use it to notice an edit: see DeleteSyncedWatchlistItem.

// noTags matches items without tags, however that is stored.
var noTags = bson.A{
	bson.M{"tags": nil}, // missing or null
	bson.M{"tags": bson.M{"$size": 0}},
}

// SetWatchlistItemIMDbID records the IMDb ID of an item that has none. It
// reports false, with no error, when the item already has one or another of
// the user's items holds that ID (the unique index refuses it).
func (s *service) SetWatchlistItemIMDbID(ctx context.Context, userID, itemID primitive.ObjectID, imdbID string) (bool, error) {
	if imdbID == "" {
		return false, nil
	}
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	res, err := coll.UpdateOne(ctx, bson.M{
		"_id":     itemID,
		"user_id": userID,
		"imdb_id": bson.M{"$in": bson.A{nil, ""}},
	}, bson.M{"$set": bson.M{"imdb_id": imdbID}})
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// MarkWatchlistItemIMDbMissing sets imdb_missing_since on an item the sync
// may later remove: one it added, that is still "planned" and has no tags.
// These are the same conditions the delete checks, so an edit the user makes
// between the plan and this write wins here too, and an item the user has
// made theirs never carries a strike. An item that is already marked keeps
// its mark: the first strike keeps its time. It reports whether the mark was
// written.
func (s *service) MarkWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID, since time.Time) (bool, error) {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	res, err := coll.UpdateOne(ctx, bson.M{
		"_id":                itemID,
		"user_id":            userID,
		"source":             models.SourceIMDb,
		"status":             models.WatchlistStatusPlanned,
		"imdb_missing_since": nil,
		"$or":                noTags,
	}, bson.M{"$set": bson.M{"imdb_missing_since": since}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// ClearWatchlistItemIMDbMissing removes the mark again. It reports whether
// the item was marked.
func (s *service) ClearWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID) (bool, error) {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	res, err := coll.UpdateOne(ctx, bson.M{
		"_id":                itemID,
		"user_id":            userID,
		"imdb_missing_since": bson.M{"$ne": nil},
	}, bson.M{"$unset": bson.M{"imdb_missing_since": ""}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// DeleteSyncedWatchlistItem deletes an item on behalf of the IMDb sync, and
// only if, at the moment of the delete, the item still is what the sync is
// allowed to remove: added by the sync, status still "planned", no tags,
// still carrying the missing mark the caller read, and not edited since the
// caller read it. It reports whether the item was deleted; false means the
// user (or another run) changed it first and it stays.
//
// updatedAt is the item's updated_at as the caller read it. Every edit a
// user makes sets updated_at, whichever field it changes, so requiring the
// same value means any edit made after the plan was drawn up wins, not only
// one to status or tags. (The field has millisecond resolution. An edit in
// the very millisecond of the write the caller read would go unnoticed by
// this one condition; status and tags are still checked for themselves.)
func (s *service) DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince, updatedAt time.Time) (bool, error) {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	res, err := coll.DeleteOne(ctx, bson.M{
		"_id":                itemID,
		"user_id":            userID,
		"source":             models.SourceIMDb,
		"status":             models.WatchlistStatusPlanned,
		"imdb_missing_since": missingSince,
		"updated_at":         updatedAt,
		"$or":                noTags,
	})
	if err != nil {
		return false, err
	}
	return res.DeletedCount == 1, nil
}
