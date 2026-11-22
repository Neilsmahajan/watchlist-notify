package database

import (
	"context"
	"errors"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func (s *service) UpsertUser(ctx context.Context, u *models.User) error {
	collection := s.db.Database(databaseName).Collection(collUsers)
	now := time.Now()
	setOnInsert := bson.M{
		"created_at": now,
		"region":     defaultRegion,
		"services":   bson.A{},
		"preferences": bson.M{
			"digest_consent":    false,
			"marketing_consent": false,
			"use_account_email": true,
			"digest": bson.M{
				"enabled":       false,
				"interval":      1,
				"interval_unit": "days",
			},
		},
	}
	set := bson.M{
		"email":      u.Email,
		"name":       u.Name,
		"picture":    u.Picture,
		"updated_at": now,
	}
	if u.Region != "" {
		// One path cannot be in both $set and $setOnInsert.
		set["region"] = u.Region
		delete(setOnInsert, "region")
	}
	_, err := collection.UpdateOne(ctx, bson.M{"email": u.Email}, bson.M{
		"$set":         set,
		"$setOnInsert": setOnInsert,
	}, options.Update().SetUpsert(true))
	return err
}

func (s *service) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	var user models.User
	err := collection.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *service) UpdateServices(ctx context.Context, userID primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	now := time.Now()
	for i := range services {
		if services[i].AddedAt.IsZero() {
			services[i].AddedAt = now
		}
	}
	var updated models.User
	res := collection.FindOneAndUpdate(ctx, bson.M{"_id": userID}, bson.M{
		"$set": bson.M{
			"services":   services,
			"updated_at": now,
		},
	}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) UpdateUserPreferences(ctx context.Context, userID primitive.ObjectID, fields map[string]any) (*models.User, error) {
	if len(fields) == 0 {
		return nil, errors.New("no fields to update")
	}
	collection := s.db.Database(databaseName).Collection(collUsers)
	now := time.Now()
	fields["updated_at"] = now
	var updated models.User
	res := collection.FindOneAndUpdate(ctx, bson.M{"_id": userID}, bson.M{
		"$set": fields,
	}, options.FindOneAndUpdate().SetReturnDocument(options.After))
	if err := res.Decode(&updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

// GetUsersForDigest returns users who opted into digests and are due: their
// next_scheduled_at has passed or was never set.
func (s *service) GetUsersForDigest(ctx context.Context, now time.Time) ([]*models.User, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)

	filter := bson.M{
		"preferences.digest_consent": true,
		"preferences.digest.enabled": true,
		"$or": []bson.M{
			{"preferences.digest.next_scheduled_at": bson.M{"$lte": now}},
			{"preferences.digest.next_scheduled_at": nil}, // null or missing
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var users []*models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

// CompareAndSwapDigestSchedule moves a user's digest schedule from one state
// to another, but only if the stored state still equals from. It reports
// whether the swap happened.
//
// The digest job uses it twice: to claim a user before sending (from: what it
// read, to: sent now, next send scheduled) and to hand the claim back when the
// send definitely failed (the reverse). Because the swap is a single atomic
// update, two overlapping job runs cannot both claim the same user.
//
// With requireEnabled the swap also requires digests to still be enabled and
// consented to, so a user who opted out a moment ago is not emailed.
func (s *service) CompareAndSwapDigestSchedule(ctx context.Context, userID primitive.ObjectID, from, to models.DigestSchedule, requireEnabled bool) (bool, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)

	const (
		lastSentField = "preferences.digest.last_sent_at"
		nextField     = "preferences.digest.next_scheduled_at"
	)
	filter := bson.M{
		"_id":         userID,
		lastSentField: timeOrNull(from.LastSentAt),
		nextField:     timeOrNull(from.NextScheduledAt),
	}
	if requireEnabled {
		filter["preferences.digest_consent"] = true
		filter["preferences.digest.enabled"] = true
	}

	set := bson.M{"updated_at": time.Now()}
	unset := bson.M{}
	for field, value := range map[string]*time.Time{lastSentField: to.LastSentAt, nextField: to.NextScheduledAt} {
		if value == nil {
			unset[field] = ""
		} else {
			set[field] = *value
		}
	}
	update := bson.M{"$set": set}
	if len(unset) > 0 {
		update["$unset"] = unset
	}

	res, err := collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, err
	}
	return res.MatchedCount == 1, nil
}

// timeOrNull builds an equality condition for an optional time. A nil filter
// value matches documents where the field is null or absent.
func timeOrNull(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

// DistinctUserRegions returns the valid regions users are in.
func (s *service) DistinctUserRegions(ctx context.Context) ([]string, error) {
	collection := s.db.Database(databaseName).Collection(collUsers)
	values, err := collection.Distinct(ctx, "region", bson.M{})
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(values))
	var regions []string
	for _, v := range values {
		raw, _ := v.(string)
		region, ok := models.NormalizeRegion(raw)
		if !ok || seen[region] {
			continue
		}
		seen[region] = true
		regions = append(regions, region)
	}
	return regions, nil
}
