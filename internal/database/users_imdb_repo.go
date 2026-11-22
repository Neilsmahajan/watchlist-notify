package database

import (
	"context"
	"errors"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ErrUserNotFound is returned when a write addresses a user that does not exist.
var ErrUserNotFound = errors.New("user not found")

// notAttemptedSince matches users whose IMDb list was not fetched after cutoff.
func notAttemptedSince(cutoff time.Time) bson.A {
	return bson.A{
		bson.M{"imdb_attempt_at": nil}, // never
		bson.M{"imdb_attempt_at": bson.M{"$lte": cutoff}},
	}
}

// ClaimIMDbAttempt records that a fetch of the user's IMDb list starts now,
// unless one started less than minGap ago. It reports whether the caller may
// fetch.
//
// It is one atomic update, so two overlapping worker runs, or a double-clicked
// link request, cannot both fetch. That is what keeps the service to the one
// request a day per user it promises to make against IMDb.
func (s *service) ClaimIMDbAttempt(ctx context.Context, userID primitive.ObjectID, now time.Time, minGap time.Duration) (bool, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	res, err := collection.UpdateOne(ctx, bson.M{
		"_id": userID,
		"$or": notAttemptedSince(now.Add(-minGap)),
	}, bson.M{"$set": bson.M{"imdb_attempt_at": now}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// LinkIMDb stores the user's link to an IMDb list, replacing any earlier one.
func (s *service) LinkIMDb(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) error {
	collection := s.db.Database(databaseName).Collection(collUsers)
	res, err := collection.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{
		"$set": bson.M{"imdb_sync": sync, "updated_at": time.Now()},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GetIMDbLink returns the user's link as it is stored right now, or nil when
// they have none (or do not exist). A sync run reads it again just before it
// marks or removes anything, to find out whether the link it started with is
// still the one in force.
func (s *service) GetIMDbLink(ctx context.Context, userID primitive.ObjectID) (*models.IMDbSync, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	var doc struct {
		IMDbSync *models.IMDbSync `bson:"imdb_sync"`
	}
	err := collection.FindOne(ctx, bson.M{"_id": userID},
		options.FindOne().SetProjection(bson.M{"imdb_sync": 1})).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if doc.IMDbSync == nil || doc.IMDbSync.SourceID == "" {
		return nil, nil
	}
	return doc.IMDbSync, nil
}

// UpdateIMDbSync stores the outcome of a sync run, but only if the user's
// link is still the one the run was for: the same list, linked at the same
// moment. It reports false when they unlinked, linked another list, or
// unlinked and linked the same list again while the run was under way; the
// run's outcome is then dropped rather than written over their choice.
func (s *service) UpdateIMDbSync(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) (bool, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	res, err := collection.UpdateOne(ctx, bson.M{
		"_id":                 userID,
		"imdb_sync.source_id": sync.SourceID,
		"imdb_sync.linked_at": sync.LinkedAt,
	}, bson.M{"$set": bson.M{"imdb_sync": sync}})
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// UnlinkIMDb removes the user's link. Their watchlist items are not touched,
// and the fetch throttle (imdb_attempt_at) deliberately stays.
func (s *service) UnlinkIMDb(ctx context.Context, userID primitive.ObjectID) error {
	collection := s.db.Database(databaseName).Collection(collUsers)
	_, err := collection.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{
		"$unset": bson.M{"imdb_sync": ""},
		"$set":   bson.M{"updated_at": time.Now()},
	})
	return err
}

// ListIMDbLinkedUsers returns the users with a linked IMDb list whose last
// fetch attempt started at or before attemptedBefore (or who were never
// fetched). Whoever has waited longest comes first, never-fetched users
// before everyone, so a run that cannot get to all of them does not keep
// leaving out the same ones.
func (s *service) ListIMDbLinkedUsers(ctx context.Context, attemptedBefore time.Time) ([]*models.User, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	cursor, err := collection.Find(ctx, bson.M{
		"imdb_sync.source_id": bson.M{"$gt": ""},
		"$or":                 notAttemptedSince(attemptedBefore),
	}, options.Find().SetSort(bson.D{{Key: "imdb_attempt_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}
