package cache

import (
	"context"
	"os"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/redis/go-redis/v9"
)

var (
	redisUrl = os.Getenv("REDIS_URL")
)

type Service interface {
	GetSearchWatchlistItemsCache(ctx context.Context, query string, page int, includeAdult bool, language, region, forcedType string) ([]tmdb.Result, int, int, error)
	SetSearchWatchlistItemsCache(ctx context.Context, query string, page int, includeAdult bool, language, region, forcedType, bodyString string) error
}

type service struct {
	client *redis.Client
}

func New() Service {
	if redisUrl == "" {
		redisUrl = "localhost:6379"
	}
	opt, err := redis.ParseURL(redisUrl)
	if err != nil {
		panic(err)
	}
	client := redis.NewClient(opt)

	return &service{
		client: client,
	}
}
