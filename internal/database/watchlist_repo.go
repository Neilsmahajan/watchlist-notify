package database

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (s *service) CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	if item == nil {
		return errors.New("item is required")
	}
	if item.UserID == primitive.NilObjectID {
		return errors.New("user id is required")
	}
	item.Title = strings.TrimSpace(item.Title)
	if item.Title == "" {
		return errors.New("title is required")
	}
	item.Type = strings.TrimSpace(strings.ToLower(item.Type))
	if item.Type == "" {
		item.Type = models.WatchlistTypeMovie
	}
	switch item.Type {
	case models.WatchlistTypeMovie, models.WatchlistTypeShow:
	default:
		return errors.New("invalid type")
	}
	if item.Status == "" {
		item.Status = models.WatchlistStatusPlanned
	}
	if item.IMDbID != "" {
		item.IMDbID = strings.TrimSpace(item.IMDbID)
	}
	if item.Year < 0 {
		return errors.New("invalid year")
	}
	if len(item.Tags) > 0 {
		tags := make([]string, 0, len(item.Tags))
		seen := make(map[string]struct{}, len(item.Tags))
		for _, tag := range item.Tags {
			trimmed := strings.TrimSpace(tag)
			if trimmed == "" {
				continue
			}
			if _, ok := seen[trimmed]; ok {
				continue
			}
			seen[trimmed] = struct{}{}
			tags = append(tags, trimmed)
		}
		item.Tags = tags
	}
	now := time.Now()
	item.AddedAt = now
	item.UpdatedAt = now
	res, err := coll.InsertOne(ctx, item)
	if err != nil {
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
	coll := s.db.Database(databaseName).Collection(collWatchlist)
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
	defer func(cursor *mongo.Cursor, ctx context.Context) {
		err := cursor.Close(ctx)
		if err != nil {
			fmt.Println("Error closing cursor:", err)
		}
	}(cursor, ctx)

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

func buildWatchlistFilter(opts models.ListWatchlistOptions) bson.M {
	filter := bson.M{"user_id": opts.UserID}
	if opts.Status != "" {
		filter["status"] = opts.Status
	}
	if opts.Type != "" {
		filter["type"] = opts.Type
	}
	if opts.Search != "" {
		// The search term is user input: match it literally, not as a regex.
		filter["title"] = bson.M{"$regex": regexp.QuoteMeta(opts.Search), "$options": "i"}
	}
	return filter
}

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
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	if len(fields) == 0 {
		return nil, errors.New("no fields to update")
	}
	fields["updated_at"] = time.Now()
	var updated models.WatchlistItem
	res := coll.FindOneAndUpdate(ctx, bson.M{"_id": itemID, "user_id": userID}, bson.M{"$set": fields}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if res.Err() != nil {
		if errors.Is(res.Err(), mongo.ErrNoDocuments) {
			return nil, ErrWatchlistItemNotFound
		}
		// Map duplicate key errors to domain error for nicer 409 handling
		var we mongo.WriteException
		if errors.As(res.Err(), &we) {
			for _, e := range we.WriteErrors {
				if e.Code == 11000 {
					return nil, ErrDuplicateWatchlistItem
				}
			}
		}
		// Some drivers surface duplicate as CommandError
		var ce mongo.CommandError
		if errors.As(res.Err(), &ce) {
			if ce.Code == 11000 {
				return nil, ErrDuplicateWatchlistItem
			}
		}
		return nil, res.Err()
	}
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) DeleteWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID) error {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	res, err := coll.DeleteOne(ctx, bson.M{"_id": itemID, "user_id": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrWatchlistItemNotFound
	}
	return nil
}

// DistinctWatchlistTitles returns every TMDb title that is on at least one
// watchlist. Items without a TMDb ID cannot be tracked and are left out.
func (s *service) DistinctWatchlistTitles(ctx context.Context) ([]models.TitleRef, error) {
	coll := s.db.Database(databaseName).Collection(collWatchlist)
	cursor, err := coll.Aggregate(ctx, mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"tmdb_id": bson.M{"$gt": 0}}}},
		{{Key: "$group", Value: bson.M{"_id": bson.M{"type": "$type", "tmdb_id": "$tmdb_id"}}}},
	})
	if err != nil {
		return nil, err
	}
	var rows []struct {
		ID struct {
			Type   string `bson:"type"`
			TMDbID int    `bson:"tmdb_id"`
		} `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	refs := make([]models.TitleRef, 0, len(rows))
	for _, row := range rows {
		mediaType, ok := models.TMDbMediaType(row.ID.Type)
		if !ok {
			continue
		}
		refs = append(refs, models.TitleRef{MediaType: mediaType, TMDbID: row.ID.TMDbID})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key() < refs[j].Key() })
	return refs, nil
}
