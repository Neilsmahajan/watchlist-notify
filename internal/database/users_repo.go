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
		"google_id":  u.GoogleID,
		"created_at": now,
		"region":     defaultRegion,
		"services":   bson.A{},
		"preferences": bson.M{
			"digest_frequency":  models.DigestFrequencyDaily,
			"digest_consent":    false,
			"marketing_consent": false,
			"use_account_email": true,
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
	if u.Preferences.DigestFrequency != "" {
		set["preferences.digest_frequency"] = u.Preferences.DigestFrequency
	}
	_, err := collection.UpdateOne(ctx, bson.M{"google_id": u.GoogleID}, bson.M{
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
		if err == mongo.ErrNoDocuments {
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
