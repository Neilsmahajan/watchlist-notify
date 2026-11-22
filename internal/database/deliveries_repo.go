package database

import (
	"context"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// InsertDigestDeliveries records that the given events are going out to the
// given users. A record that already exists for a user and event is left as
// it is; the return value is how many records were new.
func (s *service) InsertDigestDeliveries(ctx context.Context, deliveries []models.DigestDelivery) (int, error) {
	if len(deliveries) == 0 {
		return 0, nil
	}
	docs := make([]any, len(deliveries))
	for i := range deliveries {
		docs[i] = deliveries[i]
	}
	return insertNew(ctx, s.db.Database(databaseName).Collection(collDelivered), docs)
}

// ListDeliveredEventIDs returns which of eventIDs already have a delivery
// record for the user.
func (s *service) ListDeliveredEventIDs(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (map[string]bool, error) {
	delivered := make(map[string]bool)
	if len(eventIDs) == 0 {
		return delivered, nil
	}
	coll := s.db.Database(databaseName).Collection(collDelivered)
	cursor, err := coll.Find(ctx,
		bson.M{"user_id": userID, "event_id": bson.M{"$in": eventIDs}},
		options.Find().SetProjection(bson.M{"_id": 0, "event_id": 1}))
	if err != nil {
		return nil, err
	}
	var rows []struct {
		EventID string `bson:"event_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		delivered[row.EventID] = true
	}
	return delivered, nil
}

// DeleteDigestDeliveries removes the user's delivery records for eventIDs and
// reports how many there were. The digest calls it when a send is known to
// have failed, so that the next run announces those events after all.
func (s *service) DeleteDigestDeliveries(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (int, error) {
	if len(eventIDs) == 0 {
		return 0, nil
	}
	coll := s.db.Database(databaseName).Collection(collDelivered)
	res, err := coll.DeleteMany(ctx, bson.M{"user_id": userID, "event_id": bson.M{"$in": eventIDs}})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}
