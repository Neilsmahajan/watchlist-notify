package database

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
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
	UpdateServices(ctx context.Context, userID primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error)
	UpdateUserPreferences(ctx context.Context, userID primitive.ObjectID, fields map[string]any) (*models.User, error)
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
	mongoURI             = os.Getenv("MONGODB_URI") // prefer full URI if provided
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
	if mongoURI != "" { // prefer full URI if provided
		uri = mongoURI
	} else if databaseUsername != "" { // use credentials only if provided
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
			// New canonical unique index using flat field tmdb_id as stored in WatchlistItem
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "tmdb_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.D{{Key: "tmdb_id", Value: bson.D{{Key: "$exists", Value: true}}}}).SetName("uniq_user_tmdb_id"),
		},
	})
	return err
}

// watchlist repo methods moved to watchlist_repo.go
