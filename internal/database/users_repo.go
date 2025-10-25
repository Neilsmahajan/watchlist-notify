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

func (s *service) UpsertUser(ctx context.Context, u *models.User) error {
	collection := s.db.Database(databaseName).Collection("users")
	now := time.Now()
	setOnInsert := bson.M{
		"created_at": now,
		"region":     defaultRegion,
		"services":   bson.A{},
		"preferences": bson.M{
			"digest_consent":    false,
			"marketing_consent": false,
			"use_account_email": true,
			"digest": bson.M{
				"enabled":       false,
				"interval":      1,
				"interval_unit": "days",
			},
		},
	}
	set := bson.M{
		"email":      u.Email,
		"name":       u.Name,
		"picture":    u.Picture,
		"updated_at": now,
	}
	if u.Region != "" {
		set["region"] = u.Region
	}
	_, err := collection.UpdateOne(ctx, bson.M{"email": u.Email}, bson.M{
		"$set":         set,
		"$setOnInsert": setOnInsert,
	}, options.Update().SetUpsert(true))
	return err
}

func (s *service) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	collection := s.db.Database(databaseName).Collection("users")
	var user models.User
	err := collection.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *service) UpdateServices(ctx context.Context, userID primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error) {
	collection := s.db.Database(databaseName).Collection("users")
	now := time.Now()
	for i := range services {
		if services[i].AddedAt.IsZero() {
			services[i].AddedAt = now
		}
	}
	var updated models.User
	res := collection.FindOneAndUpdate(ctx, bson.M{"_id": userID}, bson.M{
		"$set": bson.M{
			"services":   services,
			"updated_at": now,
		},
	}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) UpdateUserPreferences(ctx context.Context, userID primitive.ObjectID, fields map[string]any) (*models.User, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields to update")
	}
	collection := s.db.Database(databaseName).Collection("users")
	now := time.Now()
	fields["updated_at"] = now
	var updated models.User
	res := collection.FindOneAndUpdate(ctx, bson.M{"_id": userID}, bson.M{
		"$set": fields,
	}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) GetUsersForDigest(ctx context.Context, now time.Time) ([]*models.User, error) {
	collection := s.db.Database(databaseName).Collection("users")

	filter := bson.M{
		"preferences.digest_consent": true,
		"preferences.digest.enabled": true,
		"$or": []bson.M{
			{"preferences.digest.next_scheduled_at": bson.M{"$lte": now}},
			{"preferences.digest.next_scheduled_at": bson.M{"$exists": false}},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func (s *service) UpdateDigestTimestamps(ctx context.Context, userID primitive.ObjectID, lastSent, nextScheduled time.Time) error {
	collection := s.db.Database(databaseName).Collection("users")
	_, err := collection.UpdateOne(ctx, bson.M{"_id": userID}, bson.M{
		"$set": bson.M{
			"preferences.digest.last_sent_at":      lastSent,
			"preferences.digest.next_scheduled_at": nextScheduled,
			"updated_at":                           time.Now(),
		},
	})
	return err
}
