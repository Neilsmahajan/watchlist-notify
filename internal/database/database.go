package database

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Service interface {
	Health() map[string]string
	UpsertUser(ctx context.Context, u *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
}

type service struct {
	db *mongo.Client
}

var (
	databaseHost         = os.Getenv("DB_HOST")
	databasePort         = os.Getenv("DB_PORT")
	databaseUsername     = os.Getenv("DB_USERNAME")
	databaseRootPassword = os.Getenv("DB_ROOT_PASSWORD")
	databaseName         = os.Getenv("DB_DATABASE")
	defaultRegion        = func() string {
		if v := os.Getenv("DEFAULT_REGION"); v != "" {
			return v
		}
		return "US"
	}()
)

func New() Service {
	var uri string
	if databaseUsername != "" { // use credentials only if provided
		uri = fmt.Sprintf("mongodb://%s:%s@%s:%s", databaseUsername, databaseRootPassword, databaseHost, databasePort)
	} else {
		uri = fmt.Sprintf("mongodb://%s:%s", databaseHost, databasePort)
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		log.Fatal(err)
	}

	// Ensure indexes (panic on failure so we notice early)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ensureIndexes(ctx, client.Database(databaseName)); err != nil {
		log.Fatalf("failed to create indexes: %v", err)
	}
	return &service{
		db: client,
	}
}

func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := s.db.Ping(ctx, nil)
	if err != nil {
		log.Fatalf("db down: %v", err)
	}

	return map[string]string{
		"message": "It's healthy",
	}
}

// ensureIndexes creates required indexes for phase 1 collections.
func ensureIndexes(ctx context.Context, db *mongo.Database) error {
	users := db.Collection("users")
	// Unique email
	_, err := users.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "email", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_email"),
		},
		{
			Keys:    bson.D{{Key: "google_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_google_id"),
		},
		{
			Keys:    bson.D{{Key: "region", Value: 1}}, // helpful for region cohort jobs later
			Options: options.Index().SetName("region_idx"),
		},
	})
	if err != nil {
		return err
	}
	// Watchlist indexes will be created lazily when collection used (optional to pre-create here)
	watchlist := db.Collection("watchlist_items")
	_, err = watchlist.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}},
			Options: options.Index().SetName("user_status_idx"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "external_ids.tmdb", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.D{{Key: "external_ids.tmdb", Value: bson.D{{Key: "$exists", Value: true}}}}).SetName("uniq_user_tmdb"),
		},
	})
	return err
}

func (s *service) UpsertUser(ctx context.Context, u *models.User) error {
	collection := s.db.Database(databaseName).Collection("users")
	now := time.Now()
	// Build defaults for first-time insert
	setOnInsert := bson.M{
		"google_id":  u.GoogleID,
		"created_at": now,
		"region":     defaultRegion,
		// empty slice to avoid null in JSON if fetched immediately after insert
		"services": bson.A{},
		"preferences": bson.M{
			"digest_frequency":  models.DigestFrequencyDaily,
			"digest_consent":    false,
			"marketing_consent": false,
			"use_account_email": true,
		},
	}

	// Always update these fields
	set := bson.M{
		"email":      u.Email,
		"name":       u.Name,
		"picture":    u.Picture,
		"updated_at": now,
	}

	// If caller supplied a region or preferences (e.g. post-registration update before first insert)
	if u.Region != "" {
		set["region"] = u.Region
	}
	// Only override preferences on upsert if provided AND not already set (we can't know existing without extra read, so we only allow on insert unless explicit update API is built).
	if u.Preferences.DigestFrequency != "" {
		// Provide minimal subset; full update should use a dedicated update method.
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
