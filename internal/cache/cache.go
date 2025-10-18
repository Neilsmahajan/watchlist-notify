package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisAddr = "redis://localhost:6379"
	searchKeyPrefix  = "search:v1"
	searchTTL        = 30 * time.Minute
)

var ErrMiss = errors.New("cache miss")

type Service interface {
	GetSearchResults(ctx context.Context, key SearchKey) (*SearchValue, error)
	SetSearchResults(ctx context.Context, key SearchKey, value SearchValue) error
}

type service struct {
	client redis.UniversalClient
	ttl    time.Duration
}

type noopService struct{}

type SearchKey struct {
	Query        string
	Page         int
	IncludeAdult bool
	Language     string
	Region       string
	Type         string
}

type SearchValue struct {
	Results    []tmdb.Result `json:"results"`
	Page       int           `json:"page"`
	TotalPages int           `json:"total_pages"`
}

func New() Service {
	addr := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if strings.EqualFold(addr, "disabled") {
		log.Println("cache: redis cache disabled via REDIS_URL=disabled")
		return noopService{}
	}
	if addr == "" {
		addr = defaultRedisAddr
		log.Printf("cache: REDIS_URL not set, defaulting to %s", defaultRedisAddr)
	}

	client, err := configureRedis(addr)
	if err != nil {
		log.Printf("cache: redis configuration failed (%v), disabling cache", err)
		return noopService{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("cache: redis ping failed (%v), disabling cache", err)
		return noopService{}
	}

	return &service{client: client, ttl: searchTTL}
}

func configureRedis(addr string) (redis.UniversalClient, error) {
	normalized := strings.TrimSpace(addr)
	if normalized == "" {
		normalized = defaultRedisAddr
	}
	if strings.HasPrefix(normalized, "redis://") || strings.HasPrefix(normalized, "rediss://") {
		opt, err := redis.ParseURL(normalized)
		if err != nil {
			return nil, err
		}
		return redis.NewClient(opt), nil
	}
	return redis.NewClient(&redis.Options{Addr: normalized}), nil
}

func (s *service) GetSearchResults(ctx context.Context, key SearchKey) (*SearchValue, error) {
	raw, err := s.client.Get(ctx, key.String()).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrMiss
	}
	if err != nil {
		return nil, err
	}

	var payload SearchValue
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func (s *service) SetSearchResults(ctx context.Context, key SearchKey, value SearchValue) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key.String(), data, s.ttl).Err()
}

func (noopService) GetSearchResults(context.Context, SearchKey) (*SearchValue, error) {
	return nil, ErrMiss
}

func (noopService) SetSearchResults(context.Context, SearchKey, SearchValue) error {
	return nil
}

func (k SearchKey) String() string {
	query := strings.ToLower(strings.TrimSpace(k.Query))
	language := strings.ToLower(strings.TrimSpace(k.Language))
	region := strings.ToUpper(strings.TrimSpace(k.Region))
	mediaType := strings.ToLower(strings.TrimSpace(k.Type))

	if region == "" {
		region = "-"
	}
	if language == "" {
		language = "-"
	}

	var b strings.Builder
	b.Grow(len(query) + len(language) + len(region) + len(mediaType) + 32)
	b.WriteString(searchKeyPrefix)
	b.WriteString(":q=")
	b.WriteString(query)
	b.WriteString(":p=")
	b.WriteString(strconv.Itoa(k.Page))
	b.WriteString(":adult=")
	b.WriteString(strconv.FormatBool(k.IncludeAdult))
	b.WriteString(":lang=")
	b.WriteString(language)
	b.WriteString(":region=")
	b.WriteString(region)
	b.WriteString(":type=")
	b.WriteString(mediaType)
	return b.String()
}
