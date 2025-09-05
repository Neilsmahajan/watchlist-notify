package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Service interface {
	Health() map[string]string
	UpsertUser(ctx context.Context, u *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	CountWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) (int64, error)
	UpdateWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error)
	DeleteWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID) error
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

var (
	// ErrDuplicateWatchlistItem returned when unique constraint (user + tmdb id) violated
	ErrDuplicateWatchlistItem = errors.New("watchlist item already exists for user")
	// ErrWatchlistItemNotFound when update target doesn't exist or not owned by user
	ErrWatchlistItemNotFound = errors.New("watchlist item not found")
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
	// Watchlist indexes
	watchlist := db.Collection("watchlist_items")
	_, err = watchlist.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Query patterns: user_id + status filter + sort added_at desc; listing by user only; duplicates by tmdb
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "status", Value: 1}, {Key: "added_at", Value: -1}},
			Options: options.Index().SetName("user_status_addedat_idx"),
		},
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "added_at", Value: -1}},
			Options: options.Index().SetName("user_addedat_idx"),
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

func (s *service) CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error {
	coll := s.db.Database(databaseName).Collection("watchlist_items")
	now := time.Now()
	item.AddedAt = now
	item.UpdatedAt = now
	// Defensive defaults
	if item.Status == "" {
		item.Status = models.WatchlistStatusPlanned
	}
	res, err := coll.InsertOne(ctx, item)
	if err != nil {
		// Detect duplicate key (user + tmdb id) -> 11000
		var we mongo.WriteException
		if errors.As(err, &we) {
			for _, e := range we.WriteErrors {
				if e.Code == 11000 {
					return ErrDuplicateWatchlistItem
				}
			}
		}
		return err
	}
	if oid, ok := res.InsertedID.(primitive.ObjectID); ok {
		item.ID = oid
	}
	return nil
}

func (s *service) ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error) {
	coll := s.db.Database(databaseName).Collection("watchlist_items")
	var items []*models.WatchlistItem
	filter := buildWatchlistFilter(opts)
	sort := resolveWatchlistSort(opts.Sort)
	findOpts := options.Find().SetSort(sort)
	if opts.Limit > 0 {
		findOpts.SetLimit(int64(opts.Limit))
	}
	if opts.Offset > 0 {
		findOpts.SetSkip(int64(opts.Offset))
	}
	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var item models.WatchlistItem
		if err := cursor.Decode(&item); err != nil {
			return nil, err
		}
		items = append(items, &item)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *service) CountWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) (int64, error) {
	coll := s.db.Database(databaseName).Collection("watchlist_items")
	filter := buildWatchlistFilter(opts)
	return coll.CountDocuments(ctx, filter)
}

// buildWatchlistFilter constructs a Mongo filter from list options
func buildWatchlistFilter(opts models.ListWatchlistOptions) bson.M {
	filter := bson.M{"user_id": opts.UserID}
	if opts.Status != "" {
		filter["status"] = opts.Status
	}
	if opts.Type != "" {
		filter["type"] = opts.Type
	}
	if opts.Search != "" {
		filter["title"] = bson.M{"$regex": opts.Search, "$options": "i"}
	}
	return filter
}

// resolveWatchlistSort parses sort strings like "-added_at", "title", "-year"
func resolveWatchlistSort(sortStr string) bson.D {
	if sortStr == "" {
		return bson.D{{Key: "added_at", Value: -1}}
	}
	field := sortStr
	dir := 1
	if strings.HasPrefix(sortStr, "-") {
		dir = -1
		field = strings.TrimPrefix(sortStr, "-")
	}
	switch field {
	case "added_at", "title", "year", "status":
		return bson.D{{Key: field, Value: dir}}
	default:
		return bson.D{{Key: "added_at", Value: -1}}
	}
}

func (s *service) UpdateWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error) {
	coll := s.db.Database(databaseName).Collection("watchlist_items")
	if len(fields) == 0 {
		return nil, errors.New("no fields to update")
	}
	fields["updated_at"] = time.Now()
	var updated models.WatchlistItem
	res := coll.FindOneAndUpdate(ctx, bson.M{"_id": itemID, "user_id": userID}, bson.M{"$set": fields}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if res.Err() != nil {
		if res.Err() == mongo.ErrNoDocuments {
			return nil, ErrWatchlistItemNotFound
		}
		return nil, res.Err()
	}
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) DeleteWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID) error {
	coll := s.db.Database(databaseName).Collection("watchlist_items")
	res, err := coll.DeleteOne(ctx, bson.M{"_id": itemID, "user_id": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrWatchlistItemNotFound
	}
	return nil
}
