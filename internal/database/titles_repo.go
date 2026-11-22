package database

import (
	"context"
	"errors"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// titleMetadataFields are the non-provider fields of a title document, used
// to build a projection that still limits provider snapshots to some regions.
var titleMetadataFields = []string{
	"media_type", "tmdb_id", "title", "year", "poster_path", "imdb_id", "runtime",
	"genres", "rating", "vote_count", "overview", "metadata_fetched_at",
	"not_found_at", "created_at", "updated_at",
}

// GetTitles loads titles by key in one query. When regions is non-empty only
// those regions' provider snapshots are returned, which keeps a 239-title
// read to a few hundred kilobytes. Keys without a document are simply absent
// from the result.
func (s *service) GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error) {
	titles := make(map[string]*models.Title, len(keys))
	if len(keys) == 0 {
		return titles, nil
	}
	coll := s.db.Database(databaseName).Collection(collTitles)

	findOpts := options.Find()
	if len(regions) > 0 {
		projection := bson.M{}
		for _, field := range titleMetadataFields {
			projection[field] = 1
		}
		for _, raw := range regions {
			// Regions end up in a field path, so only validated codes get through.
			if region, ok := models.NormalizeRegion(raw); ok {
				projection["providers."+region] = 1
			}
		}
		findOpts.SetProjection(projection)
	}

	cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": keys}}, findOpts)
	if err != nil {
		return nil, err
	}
	var docs []*models.Title
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for _, doc := range docs {
		titles[doc.ID] = doc
	}
	return titles, nil
}

// GetTitlesByIMDbIDs finds stored titles by IMDb ID, keyed by that ID, without
// their provider snapshots. It answers "which TMDb title is tt0133093" from
// what is already stored, so the IMDb sync and the CSV import ask TMDb only
// about titles nobody has listed before. Titles TMDb no longer has are left
// out. IDs without a stored title are absent from the result.
func (s *service) GetTitlesByIMDbIDs(ctx context.Context, imdbIDs []string) (map[string]*models.Title, error) {
	titles := make(map[string]*models.Title, len(imdbIDs))
	if len(imdbIDs) == 0 {
		return titles, nil
	}
	coll := s.db.Database(databaseName).Collection(collTitles)

	projection := bson.M{}
	for _, field := range titleMetadataFields {
		projection[field] = 1
	}
	// Sorted by _id so that, should two titles ever claim one IMDb ID, the
	// same one wins every time.
	cursor, err := coll.Find(ctx, bson.M{
		"imdb_id":      bson.M{"$in": imdbIDs},
		"not_found_at": nil,
	}, options.Find().SetProjection(projection).SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var docs []*models.Title
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	for _, doc := range docs {
		if _, taken := titles[doc.IMDbID]; !taken {
			titles[doc.IMDbID] = doc
		}
	}
	return titles, nil
}

// SaveTitleSnapshot stores a region's provider snapshot with a
// compare-and-set on the snapshot's version (its providers_fetched_at):
//
//   - prevVersion == nil writes a baseline, and only if the title has no
//     snapshot for the region yet. The title document is created if needed.
//   - Otherwise the stored snapshot must still carry prevVersion.
//
// It returns false, with no error, when the condition does not hold because
// another writer got there first. meta, when given, is written alongside.
func (s *service) SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prevVersion *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error) {
	region, ok := models.NormalizeRegion(region)
	if !ok {
		return false, errors.New("invalid region")
	}
	coll := s.db.Database(databaseName).Collection(collTitles)
	now := time.Now().UTC()
	snapshotField := "providers." + region

	set := bson.M{
		snapshotField: snap,
		"media_type":  ref.MediaType,
		"tmdb_id":     ref.TMDbID,
		"updated_at":  now,
	}
	if meta != nil {
		set["title"] = meta.Title
		set["year"] = meta.Year
		set["poster_path"] = meta.PosterPath
		set["imdb_id"] = meta.IMDbID
		set["runtime"] = meta.Runtime
		set["genres"] = meta.Genres
		set["rating"] = meta.Rating
		set["vote_count"] = meta.VoteCount
		set["overview"] = meta.Overview
		set["metadata_fetched_at"] = meta.MetadataFetchedAt
	}
	update := bson.M{
		"$set":   set,
		"$unset": bson.M{"not_found_at": ""},
	}

	if prevVersion != nil {
		res, err := coll.UpdateOne(ctx, bson.M{
			"_id":                                   ref.Key(),
			snapshotField + ".providers_fetched_at": *prevVersion,
		}, update)
		if err != nil {
			return false, err
		}
		return res.MatchedCount == 1, nil
	}

	// Baseline. If the document already has a snapshot for the region the
	// filter does not match, the upsert tries to insert a second document
	// with the same _id, and the duplicate-key error tells us we lost.
	update["$setOnInsert"] = bson.M{"created_at": now}
	_, err := coll.UpdateOne(ctx, bson.M{
		"_id":         ref.Key(),
		snapshotField: bson.M{"$exists": false},
	}, update, options.Update().SetUpsert(true))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// MarkTitleNotFound records that TMDb no longer has the title. Stored
// snapshots are left untouched.
func (s *service) MarkTitleNotFound(ctx context.Context, ref models.TitleRef, at time.Time) error {
	coll := s.db.Database(databaseName).Collection(collTitles)
	_, err := coll.UpdateOne(ctx, bson.M{"_id": ref.Key()}, bson.M{
		"$set": bson.M{
			"media_type":   ref.MediaType,
			"tmdb_id":      ref.TMDbID,
			"not_found_at": at,
			"updated_at":   at,
		},
		"$setOnInsert": bson.M{"created_at": at},
	}, options.Update().SetUpsert(true))
	return err
}

// InsertAvailabilityEvents inserts events and reports how many were new.
// Event IDs are deterministic, so an event that already exists is skipped
// rather than treated as an error; that is what makes re-running a diff safe.
func (s *service) InsertAvailabilityEvents(ctx context.Context, events []models.AvailabilityEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	docs := make([]any, len(events))
	for i := range events {
		docs[i] = events[i]
	}
	return insertNew(ctx, s.db.Database(databaseName).Collection(collEvents), docs)
}

// insertNew inserts docs, skipping the ones that collide with a unique index,
// and reports how many were actually inserted. Unordered, so one duplicate
// does not stop the documents after it.
func insertNew(ctx context.Context, coll *mongo.Collection, docs []any) (int, error) {
	_, err := coll.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	if err == nil {
		return len(docs), nil
	}
	var bulkErr mongo.BulkWriteException
	if !errors.As(err, &bulkErr) || bulkErr.WriteConcernError != nil {
		return 0, err
	}
	duplicates := 0
	for _, writeErr := range bulkErr.WriteErrors {
		if writeErr.Code != duplicateKeyCode {
			return 0, err
		}
		duplicates++
	}
	return len(docs) - duplicates, nil
}

const duplicateKeyCode = 11000

// ListAvailabilityEvents returns the events for the given titles in a region
// detected at or after since, oldest first.
func (s *service) ListAvailabilityEvents(ctx context.Context, titleKeys []string, region string, since time.Time) ([]models.AvailabilityEvent, error) {
	if len(titleKeys) == 0 {
		return nil, nil
	}
	coll := s.db.Database(databaseName).Collection(collEvents)
	cursor, err := coll.Find(ctx, bson.M{
		"region":      region,
		"title_key":   bson.M{"$in": titleKeys},
		"detected_at": bson.M{"$gte": since},
	}, options.Find().SetSort(bson.D{{Key: "detected_at", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var events []models.AvailabilityEvent
	if err := cursor.All(ctx, &events); err != nil {
		return nil, err
	}
	return events, nil
}
