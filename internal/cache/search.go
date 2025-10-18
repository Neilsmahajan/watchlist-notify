package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/neilsmahajan/watchlist-notify/internal/providers/tmdb"
	"github.com/redis/go-redis/v9"
)

const (
	defaultImageBase = "https://image.tmdb.org/t/p/w500"
)

func (s *service) GetSearchWatchlistItemsCache(ctx context.Context, query string, page int, includeAdult bool, language, region, forcedType string) ([]tmdb.Result, int, int, error) {
	key := s.generateSearchCacheKey(query, page, includeAdult, language, region, forcedType)
	val, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, 0, 0, nil // cache miss
	} else if err != nil {
		return nil, 0, 0, err
	}

	var sr tmdb.SearchResp
	if err = json.Unmarshal([]byte(val), &sr); err != nil {
		return nil, 0, 0, err
	}
	out := make([]tmdb.Result, 0, len(sr.Results))
	for _, r := range sr.Results {
		out = append(out, entryToResult(r, forcedType))
	}
	fmt.Printf("Cache hit for key: %s and got value: %s\n", key, val)
	return out, sr.Page, sr.TotalPages, nil
}

func (s *service) SetSearchWatchlistItemsCache(ctx context.Context, query string, page int, includeAdult bool, language, region, forcedType, bodyString string) error {
	key := s.generateSearchCacheKey(query, page, includeAdult, language, region, forcedType)

	if err := s.client.Set(ctx, key, bodyString, 0).Err(); err != nil {
		return err
	}
	fmt.Printf("Set cache for key: %s to value: %s\n", key, bodyString)
	return nil
}

func (s *service) generateSearchCacheKey(query string, page int, includeAdult bool, language, region, forcedType string) string {
	key := "search:query=" + query + ":page=" + strconv.Itoa(page) + ":adult=" + strconv.FormatBool(includeAdult) + ":lang=" + language + ":region=" + region + ":type=" + forcedType
	return key
}

func entryToResult(r tmdb.SearchEntry, forcedType string) tmdb.Result {
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.TrimSpace(r.Name)
	}
	if title == "" {
		title = strings.TrimSpace(r.OriginalTitle)
	}
	if title == "" {
		title = strings.TrimSpace(r.OriginalName)
	}
	year := yearFromDate(r.ReleaseDate)
	if year == 0 {
		year = yearFromDate(r.FirstAirDate)
	}
	mediaType := forcedType
	if mediaType == "" {
		mt := strings.ToLower(strings.TrimSpace(r.MediaType))
		if mt == "movie" || mt == "tv" {
			mediaType = mt
		}
	}
	if mediaType == "" {
		if r.FirstAirDate != "" {
			mediaType = "tv"
		} else {
			mediaType = "movie"
		}
	}
	posterURL := ""
	if r.PosterPath != "" {
		posterURL = defaultImageBase + r.PosterPath
	}
	return tmdb.Result{
		TMDBID:    r.ID,
		Title:     title,
		Year:      year,
		Type:      mediaType,
		PosterURL: posterURL,
		Poster:    r.PosterPath,
	}
}

func yearFromDate(s string) int {
	if len(s) >= 4 {
		if y, err := strconv.Atoi(s[:4]); err == nil {
			return y
		}
	}
	return 0
}
