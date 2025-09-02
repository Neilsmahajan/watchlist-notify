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
)

func New() Service {
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(fmt.Sprintf("mongodb://%s:%s@%s:%s", databaseUsername, databaseRootPassword, databaseHost, databasePort)))
	if err != nil {
		log.Fatal(err)
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

func (s *service) UpsertUser(ctx context.Context, u *models.User) error {
	collection := s.db.Database(databaseName).Collection("users")
	now := time.Now()
	_, err := collection.UpdateOne(ctx, bson.M{"google_id": u.GoogleID}, bson.M{
		"$set": bson.M{
			"email":      u.Email,
			"name":       u.Name,
			"picture":    u.Picture,
			"updated_at": now,
		},
		"$setOnInsert": bson.M{
			"google_id":  u.GoogleID,
			"created_at": now,
		},
	}, options.Update().SetUpsert(true))
	return err
}
