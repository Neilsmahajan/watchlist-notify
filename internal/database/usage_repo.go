package database

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// A usage counter is one document in usage_counters: how many times
// something that costs money has been used in one period.
//
//	{_id: "tonight:2026-11-22:total", count: 17, expires_at: <date>}
//
// The key names the period, so a new day starts on a new document and
// nothing ever has to be reset. The TTL index on expires_at removes the old
// ones.
type usageCounter struct {
	Key       string    `bson:"_id"`
	Count     int       `bson:"count"`
	ExpiresAt time.Time `bson:"expires_at"`
}

// ReserveUsage adds one to the counter under key unless that would take it
// past limit, and reports whether it did. When it did, count is the
// counter's new value.
//
// The check and the increment are one conditional write on one document, so
// any number of concurrent callers together take exactly limit reservations
// and never more:
//
//   - The filter matches the counter only while it is below the limit, and
//     the update is an upsert, so the first reservation of a period creates
//     the document with a count of one.
//   - When the counter is at the limit the filter matches nothing, the upsert
//     tries to insert a second document with the same _id, and MongoDB
//     refuses it with a duplicate-key error. The same error is what the loser
//     of two concurrent first reservations sees.
//   - So after a duplicate-key error the document is known to exist, and one
//     plain conditional increment (no upsert) settles it: it either finds
//     the counter below the limit, or the limit is reached.
//
// A limit of zero or less refuses without touching the database. expiresAt
// is stored only when the document is created.
func (s *service) ReserveUsage(ctx context.Context, key string, limit int, expiresAt time.Time) (count int, ok bool, err error) {
	if limit <= 0 {
		return 0, false, nil
	}
	coll := s.db.Database(databaseName).Collection(collUsage)
	filter := bson.M{"_id": key, "count": bson.M{"$lt": limit}}
	update := bson.M{
		"$inc":         bson.M{"count": 1},
		"$setOnInsert": bson.M{"expires_at": expiresAt},
	}

	var counter usageCounter
	err = coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&counter)
	if err == nil {
		return counter.Count, true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return 0, false, err
	}

	err = coll.FindOneAndUpdate(ctx, filter, bson.M{"$inc": bson.M{"count": 1}},
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&counter)
	switch {
	case err == nil:
		return counter.Count, true, nil
	case errors.Is(err, mongo.ErrNoDocuments):
		return 0, false, nil // at the limit
	default:
		return 0, false, err
	}
}

// ReleaseUsage gives back one reservation made with ReserveUsage. A counter
// that does not exist or is already at zero is left alone.
func (s *service) ReleaseUsage(ctx context.Context, key string) error {
	coll := s.db.Database(databaseName).Collection(collUsage)
	_, err := coll.UpdateOne(ctx,
		bson.M{"_id": key, "count": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"count": -1}})
	return err
}

// UsageCounts returns the current value of each counter. A counter that does
// not exist yet is absent from the result, which reads as zero.
func (s *service) UsageCounts(ctx context.Context, keys []string) (map[string]int, error) {
	counts := make(map[string]int, len(keys))
	if len(keys) == 0 {
		return counts, nil
	}
	coll := s.db.Database(databaseName).Collection(collUsage)
	cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": keys}},
		options.Find().SetProjection(bson.M{"count": 1}))
	if err != nil {
		return nil, err
	}
	var rows []usageCounter
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.Key] = row.Count
	}
	return counts, nil
}
