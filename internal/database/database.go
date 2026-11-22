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

// Service is the application's persistence layer. Packages that need only a
// slice of it declare their own narrower interfaces.
type Service interface {
	// Health reports whether the database answers a ping.
	Health(ctx context.Context) error

	UpsertUser(ctx context.Context, u *models.User) error
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpdateServices(ctx context.Context, userID primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error)
	UpdateUserPreferences(ctx context.Context, userID primitive.ObjectID, fields map[string]any) (*models.User, error)
	GetUsersForDigest(ctx context.Context, now time.Time) ([]*models.User, error)
	CompareAndSwapDigestSchedule(ctx context.Context, userID primitive.ObjectID, from, to models.DigestSchedule, requireEnabled bool) (bool, error)
	DistinctUserRegions(ctx context.Context) ([]string, error)

	// IMDb list link and its fetch throttle (users_imdb_repo.go).
	ClaimIMDbAttempt(ctx context.Context, userID primitive.ObjectID, now time.Time, minGap time.Duration) (bool, error)
	LinkIMDb(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) error
	GetIMDbLink(ctx context.Context, userID primitive.ObjectID) (*models.IMDbSync, error)
	UpdateIMDbSync(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) (bool, error)
	UnlinkIMDb(ctx context.Context, userID primitive.ObjectID, now time.Time, lockout time.Duration) (bool, error)
	ListIMDbLinkedUsers(ctx context.Context, attemptedBefore time.Time) ([]*models.User, error)

	CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error
	CreateWatchlistItems(ctx context.Context, items []*models.WatchlistItem) ([]*models.WatchlistItem, error)
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	UpdateWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error)
	DeleteWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID) error
	DistinctWatchlistTitles(ctx context.Context) ([]models.TitleRef, error)

	// Bookkeeping of the IMDb sync on single items (watchlist_sync_repo.go).
	// Every one is a conditional write scoped to the user.
	AdoptWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, imdbID string) (bool, error)
	MarkWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID, since time.Time) (bool, error)
	ClearWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID) (bool, error)
	ClearIMDbMissingMarks(ctx context.Context, userID primitive.ObjectID) (int, error)
	DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince, updatedAt time.Time) (bool, error)

	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
	GetTitlesByIMDbIDs(ctx context.Context, imdbIDs []string) (map[string]*models.Title, error)
	SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prevVersion *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error)
	MarkTitleNotFound(ctx context.Context, ref models.TitleRef, at time.Time) error

	InsertAvailabilityEvents(ctx context.Context, events []models.AvailabilityEvent) (int, error)
	ListAvailabilityEvents(ctx context.Context, titleKeys []string, region string, since time.Time) ([]models.AvailabilityEvent, error)

	InsertDigestDeliveries(ctx context.Context, deliveries []models.DigestDelivery) (int, error)
	ListDeliveredEventIDs(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (map[string]bool, error)
	DeleteDigestDeliveries(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (int, error)
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
	defaultRegion        = DefaultRegion()
)

var (
	// ErrDuplicateWatchlistItem is returned when a unique index on watchlist
	// items (user + type + TMDb ID, or user + IMDb ID) is violated.
	ErrDuplicateWatchlistItem = errors.New("watchlist item already exists for user")
	// ErrWatchlistItemNotFound when update target doesn't exist or not owned by user
	ErrWatchlistItemNotFound = errors.New("watchlist item not found")
)

// DefaultRegion is the region assigned to new users and always tracked by the
// availability refresh: DEFAULT_REGION when it is a valid two-letter code,
// otherwise models.DefaultRegion.
func DefaultRegion() string {
	if region, ok := models.NormalizeRegion(os.Getenv("DEFAULT_REGION")); ok {
		return region
	}
	return models.DefaultRegion
}

const (
	collUsers     = "users"
	collWatchlist = "watchlist_items"
	collTitles    = "titles"
	collEvents    = "availability_events"
	collDelivered = "digest_deliveries"
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

// Health pings the database. It returns the error instead of exiting, so a
// database blip degrades the health check without taking the server down.
func (s *service) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	return s.db.Ping(ctx, nil)
}

// Names of the unique index on a user's TMDb titles. The old one left the
// media type out, so a movie and a show that happen to share a TMDb ID (IDs
// are per media type) could not both be on one watchlist.
const (
	indexUserTypeTMDb   = "uniq_user_type_tmdb_id"
	legacyIndexUserTMDb = "uniq_user_tmdb_id"
)

// ensureIndexes creates the indexes the queries rely on. Index creation is
// idempotent and additive, with one exception: the unique index on a user's
// TMDb titles is replaced by one that includes the media type (see
// replaceLegacyTMDbIndex).
func ensureIndexes(ctx context.Context, db *mongo.Database) error {
	users := db.Collection(collUsers)
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
	watchlist := db.Collection(collWatchlist)
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
			// One item per user and TMDb title. A title is a media type plus
			// an ID: movie 1399 and show 1399 are different titles.
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "type", Value: 1}, {Key: "tmdb_id", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "tmdb_id", Value: bson.D{{Key: "$exists", Value: true}}}}).
				SetName(indexUserTypeTMDb),
		},
		{
			Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "imdb_id", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.D{{Key: "imdb_id", Value: bson.D{{Key: "$exists", Value: true}}}}).
				SetName("uniq_user_imdb_id"),
		},
	})
	if err != nil {
		return err
	}
	replaceLegacyTMDbIndex(ctx, watchlist)

	// Titles are read by _id, the deterministic title key, everywhere except
	// one place: the IMDb sync and the CSV import resolve IMDb IDs against
	// titles that are already stored before asking TMDb.
	_, err = db.Collection(collTitles).Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "imdb_id", Value: 1}},
		Options: options.Index().SetSparse(true).SetName("imdb_id_idx"),
	})
	if err != nil {
		return err
	}

	// Availability events. Event de-duplication needs no index of its own: it
	// rides on the deterministic event _id.
	events := db.Collection(collEvents)
	_, err = events.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// Digest query: events for a set of titles in a region since a time.
			Keys:    bson.D{{Key: "region", Value: 1}, {Key: "title_key", Value: 1}, {Key: "detected_at", Value: 1}},
			Options: options.Index().SetName("region_title_detected_idx"),
		},
		{
			// TTL: MongoDB deletes each event once its expires_at has passed.
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("expires_at_ttl"),
		},
	})
	if err != nil {
		return err
	}

	// Digest delivery log: which events each user has already been told about.
	deliveries := db.Collection(collDelivered)
	_, err = deliveries.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			// One record per user and event. It answers "which of these events
			// did this user already get" and makes recording idempotent.
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "event_id", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("uniq_user_event"),
		},
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("expires_at_ttl"),
		},
	})
	return err
}

// replaceLegacyTMDbIndex drops the unique index on (user_id, tmdb_id) once
// its replacement on (user_id, type, tmdb_id) exists. The caller creates the
// replacement first and calls this only when that succeeded, so there is
// never a moment without a uniqueness guarantee. The replacement always
// builds: any data the old, stricter index accepted satisfies the new one.
//
// A failed drop is logged and startup continues. Until it is dropped the old
// index only keeps rejecting what it always rejected, and the next start
// tries again. An older build that starts later (a rollback) creates the old
// index again; see the rollback note in the IMDb sync pull request.
func replaceLegacyTMDbIndex(ctx context.Context, watchlist *mongo.Collection) {
	_, err := watchlist.Indexes().DropOne(ctx, legacyIndexUserTMDb)
	if err == nil {
		log.Printf("database: dropped index %s, replaced by %s", legacyIndexUserTMDb, indexUserTypeTMDb)
		return
	}
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) && (cmdErr.Code == codeIndexNotFound || cmdErr.Code == codeNamespaceNotFound) {
		return // already gone
	}
	log.Printf("database: could not drop index %s (will retry on next start): %v", legacyIndexUserTMDb, err)
}

// MongoDB server error codes.
const (
	codeNamespaceNotFound = 26
	codeIndexNotFound     = 27
)
