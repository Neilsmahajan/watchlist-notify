// Package storetest provides an in-memory stand-in for the MongoDB-backed
// database.Service, for unit tests that must run without a database. It
// reproduces the semantics the application relies on: compare-and-set on
// snapshots and digest schedules, event de-duplication by ID, per-region
// projection, the two unique indexes on watchlist items, the conditions on
// the IMDb sync's writes, and millisecond time precision.
package storetest

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Store is an in-memory database.Service. The zero value is not usable; call
// New. Methods the fake does not implement panic through the embedded nil
// interface, which is the right failure for a test that reaches them.
type Store struct {
	database.Service

	mu     sync.Mutex
	users  map[primitive.ObjectID]*models.User
	items  []*models.WatchlistItem
	titles map[string]*models.Title
	events map[string]models.AvailabilityEvent
	// deliveries is keyed by user and event, like the unique index.
	deliveries map[deliveryKey]models.DigestDelivery

	// Err, when set, is returned by every method. It simulates an outage.
	Err error
	// Fail holds an error per method name, for a single write going wrong
	// while the rest of the database works.
	Fail map[string]error
	// Stall names methods that do not answer: a call waits until its context
	// is done and then fails with the context's error, the way a query to a
	// database that has stopped responding does. A caller that set no
	// deadline is let through after a few seconds, with the call then done
	// as usual, so that a test of a missing deadline fails instead of
	// hanging.
	Stall map[string]bool
	// Queries counts calls per method name.
	Queries map[string]int
	// Writes counts calls that changed something.
	Writes int
}

type deliveryKey struct {
	userID  primitive.ObjectID
	eventID string
}

var _ database.Service = (*Store)(nil)

// New returns an empty store.
func New() *Store {
	return &Store{
		users:      make(map[primitive.ObjectID]*models.User),
		titles:     make(map[string]*models.Title),
		events:     make(map[string]models.AvailabilityEvent),
		deliveries: make(map[deliveryKey]models.DigestDelivery),
		Queries:    make(map[string]int),
		Fail:       make(map[string]error),
		Stall:      make(map[string]bool),
	}
}

// stallCap is the longest a stalled method waits for its context to end.
const stallCap = 3 * time.Second

// begin locks the store, counts the call and reports the injected error.
// A stalled method first waits for its context to end (see Stall).
func (s *Store) begin(ctx context.Context, method string) (unlock func(), err error) {
	s.mu.Lock()
	stalled := s.Stall[method]
	s.mu.Unlock()
	if stalled {
		select {
		case <-ctx.Done():
		case <-time.After(stallCap):
		}
	}

	s.mu.Lock()
	s.Queries[method]++
	switch {
	case s.Err != nil:
		return s.mu.Unlock, s.Err
	case stalled && ctx.Err() != nil:
		return s.mu.Unlock, ctx.Err()
	}
	return s.mu.Unlock, s.Fail[method]
}

func ms(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

func msPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := ms(*t)
	return &v
}

// --- seeding and inspection ---

// AddUser stores a user, assigning an ID when it has none.
func (s *Store) AddUser(u *models.User) *models.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u.ID.IsZero() {
		u.ID = primitive.NewObjectID()
	}
	cp := *u
	s.users[u.ID] = &cp
	return u
}

// User returns the stored user.
func (s *Store) User(id primitive.ObjectID) *models.User {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneUser(s.users[id])
}

func cloneUser(u *models.User) *models.User {
	cp := *u
	if u.IMDbSync != nil {
		sync := *u.IMDbSync
		sync.LastAttemptAt, sync.LastSyncedAt = msPtr(sync.LastAttemptAt), msPtr(sync.LastSyncedAt)
		cp.IMDbSync = &sync
	}
	cp.IMDbAttemptAt = msPtr(u.IMDbAttemptAt)
	return &cp
}

// AddItem stores a watchlist item, assigning an ID when it has none.
func (s *Store) AddItem(item *models.WatchlistItem) *models.WatchlistItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item.ID.IsZero() {
		item.ID = primitive.NewObjectID()
	}
	cp := *item
	s.items = append(s.items, &cp)
	return item
}

// Items returns copies of the user's watchlist items, in insertion order.
func (s *Store) Items(userID primitive.ObjectID) []*models.WatchlistItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*models.WatchlistItem
	for _, item := range s.items {
		if item.UserID == userID {
			out = append(out, cloneItem(item))
		}
	}
	return out
}

// RemoveItem deletes a stored item, the way a user's delete arriving from
// another request would. It reports whether the item existed.
func (s *Store) RemoveItem(id primitive.ObjectID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return true
		}
	}
	return false
}

// EditItem changes a stored item in place, the way a user's edit arriving
// from another request would: like the real update it moves updated_at on,
// whatever the edit changes. It reports whether the item exists.
func (s *Store) EditItem(id primitive.ObjectID, edit func(*models.WatchlistItem)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.items {
		if item.ID == id {
			previous := ms(item.UpdatedAt)
			edit(item)
			// Always later than before, also for two edits in one millisecond.
			item.UpdatedAt = ms(time.Now())
			if !item.UpdatedAt.After(previous) {
				item.UpdatedAt = previous.Add(time.Millisecond)
			}
			return true
		}
	}
	return false
}

func cloneItem(item *models.WatchlistItem) *models.WatchlistItem {
	cp := *item
	cp.Tags = append([]string(nil), item.Tags...)
	cp.IMDbMissingSince = msPtr(item.IMDbMissingSince)
	return &cp
}

// PutTitle stores a title document as is.
func (s *Store) PutTitle(t *models.Title) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.titles[t.ID] = cloneTitle(t, nil)
}

// Title returns the stored title with every region, or nil.
func (s *Store) Title(key string) *models.Title {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.titles[key]; ok {
		return cloneTitle(t, nil)
	}
	return nil
}

// Events returns every stored event, oldest first.
func (s *Store) Events() []models.AvailabilityEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.AvailabilityEvent, 0, len(s.events))
	for _, ev := range s.events {
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DetectedAt.Equal(out[j].DetectedAt) {
			return out[i].DetectedAt.Before(out[j].DetectedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// AddEvent stores an event directly.
func (s *Store) AddEvent(ev models.AvailabilityEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev.DetectedAt = ms(ev.DetectedAt)
	s.events[ev.ID] = ev
}

// Deliveries returns the IDs of the events the user has a delivery record
// for, sorted.
func (s *Store) Deliveries(userID primitive.ObjectID) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for key := range s.deliveries {
		if key.userID == userID {
			ids = append(ids, key.eventID)
		}
	}
	sort.Strings(ids)
	return ids
}

func cloneTitle(t *models.Title, regions []string) *models.Title {
	cp := *t
	cp.Providers = make(map[string]models.RegionSnapshot, len(t.Providers))
	for region, snap := range t.Providers {
		if regions != nil && !contains(regions, region) {
			continue
		}
		snap.Offers = append([]models.ProviderOffer(nil), snap.Offers...)
		cp.Providers[region] = snap
	}
	return &cp
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// --- database.Service ---

func (s *Store) Health(ctx context.Context) error {
	unlock, err := s.begin(ctx, "Health")
	defer unlock()
	return err
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	unlock, err := s.begin(ctx, "GetUserByEmail")
	defer unlock()
	if err != nil {
		return nil, err
	}
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			return cloneUser(u), nil
		}
	}
	return nil, nil
}

// UpsertUser creates the user with that email, with the defaults the real
// upsert sets on insert, or updates the name and picture of the one that
// exists.
func (s *Store) UpsertUser(ctx context.Context, u *models.User) error {
	unlock, err := s.begin(ctx, "UpsertUser")
	defer unlock()
	if err != nil {
		return err
	}
	now := ms(time.Now())
	for _, existing := range s.users {
		if existing.Email == u.Email {
			existing.Name, existing.Picture, existing.UpdatedAt = u.Name, u.Picture, now
			if u.Region != "" {
				existing.Region = u.Region
			}
			s.Writes++
			return nil
		}
	}
	created := &models.User{
		ID:        primitive.NewObjectID(),
		Email:     u.Email,
		Name:      u.Name,
		Picture:   u.Picture,
		Region:    u.Region,
		Services:  []models.ServiceSubscription{},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if created.Region == "" {
		created.Region = database.DefaultRegion()
	}
	created.Preferences.UseAccountEmail = true
	created.Preferences.Digest.Interval = 1
	created.Preferences.Digest.IntervalUnit = "days"
	s.users[created.ID] = created
	s.Writes++
	return nil
}

func (s *Store) GetUsersForDigest(ctx context.Context, now time.Time) ([]*models.User, error) {
	unlock, err := s.begin(ctx, "GetUsersForDigest")
	defer unlock()
	if err != nil {
		return nil, err
	}
	var due []*models.User
	for _, u := range s.users {
		d := u.Preferences.Digest
		if !u.Preferences.DigestConsent || !d.Enabled {
			continue
		}
		if d.NextScheduledAt == nil || !d.NextScheduledAt.After(now) {
			cp := *u
			due = append(due, &cp)
		}
	}
	sort.Slice(due, func(i, j int) bool { return due[i].Email < due[j].Email })
	return due, nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return ms(*a).Equal(ms(*b))
}

func (s *Store) CompareAndSwapDigestSchedule(ctx context.Context, userID primitive.ObjectID, from, to models.DigestSchedule, requireEnabled bool) (bool, error) {
	unlock, err := s.begin(ctx, "CompareAndSwapDigestSchedule")
	defer unlock()
	if err != nil {
		return false, err
	}
	u, ok := s.users[userID]
	if !ok {
		return false, nil
	}
	d := &u.Preferences.Digest
	if !sameTime(d.LastSentAt, from.LastSentAt) || !sameTime(d.NextScheduledAt, from.NextScheduledAt) {
		return false, nil
	}
	if requireEnabled && (!d.Enabled || !u.Preferences.DigestConsent) {
		return false, nil
	}
	d.LastSentAt, d.NextScheduledAt = msPtr(to.LastSentAt), msPtr(to.NextScheduledAt)
	s.Writes++
	return true, nil
}

func (s *Store) DistinctUserRegions(ctx context.Context) ([]string, error) {
	unlock, err := s.begin(ctx, "DistinctUserRegions")
	defer unlock()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var regions []string
	for _, u := range s.users {
		if region, ok := models.NormalizeRegion(u.Region); ok && !seen[region] {
			seen[region] = true
			regions = append(regions, region)
		}
	}
	sort.Strings(regions)
	return regions, nil
}

// duplicateOf reports whether the user already has the item's title, under
// the same rules as the two unique indexes: (user, type, TMDb ID) and
// (user, IMDb ID).
func (s *Store) duplicateOf(item *models.WatchlistItem) bool {
	for _, existing := range s.items {
		if existing.UserID != item.UserID {
			continue
		}
		if item.TMDbID > 0 && existing.TMDbID == item.TMDbID && existing.Type == item.Type {
			return true
		}
		if item.IMDbID != "" && existing.IMDbID == item.IMDbID {
			return true
		}
	}
	return false
}

// --- IMDb link ---

func (s *Store) ClaimIMDbAttempt(ctx context.Context, userID primitive.ObjectID, now time.Time, minGap time.Duration) (bool, error) {
	unlock, err := s.begin(ctx, "ClaimIMDbAttempt")
	defer unlock()
	if err != nil {
		return false, err
	}
	u, ok := s.users[userID]
	if !ok {
		return false, nil
	}
	if u.IMDbAttemptAt != nil && u.IMDbAttemptAt.After(ms(now.Add(-minGap))) {
		return false, nil
	}
	u.IMDbAttemptAt = msPtr(&now)
	s.Writes++
	return true, nil
}

func (s *Store) LinkIMDb(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) error {
	unlock, err := s.begin(ctx, "LinkIMDb")
	defer unlock()
	if err != nil {
		return err
	}
	u, ok := s.users[userID]
	if !ok {
		return database.ErrUserNotFound
	}
	u.IMDbSync = &sync
	s.Writes++
	return nil
}

func (s *Store) GetIMDbLink(ctx context.Context, userID primitive.ObjectID) (*models.IMDbSync, error) {
	unlock, err := s.begin(ctx, "GetIMDbLink")
	defer unlock()
	if err != nil {
		return nil, err
	}
	u, ok := s.users[userID]
	if !ok || u.IMDbSync == nil || u.IMDbSync.SourceID == "" {
		return nil, nil
	}
	return cloneUser(u).IMDbSync, nil
}

func (s *Store) UpdateIMDbSync(ctx context.Context, userID primitive.ObjectID, sync models.IMDbSync) (bool, error) {
	unlock, err := s.begin(ctx, "UpdateIMDbSync")
	defer unlock()
	if err != nil {
		return false, err
	}
	u, ok := s.users[userID]
	if !ok || u.IMDbSync == nil || u.IMDbSync.SourceID != sync.SourceID || !ms(u.IMDbSync.LinkedAt).Equal(ms(sync.LinkedAt)) {
		return false, nil
	}
	u.IMDbSync = &sync
	s.Writes++
	return true, nil
}

func (s *Store) UnlinkIMDb(ctx context.Context, userID primitive.ObjectID, now time.Time, lockout time.Duration) (bool, error) {
	unlock, err := s.begin(ctx, "UnlinkIMDb")
	defer unlock()
	if err != nil {
		return false, err
	}
	u, ok := s.users[userID]
	if !ok || (u.IMDbAttemptAt != nil && u.IMDbAttemptAt.After(ms(now.Add(-lockout)))) {
		return false, nil
	}
	u.IMDbSync = nil
	s.Writes++
	return true, nil
}

func (s *Store) ListIMDbLinkedUsers(ctx context.Context, attemptedBefore time.Time) ([]*models.User, error) {
	unlock, err := s.begin(ctx, "ListIMDbLinkedUsers")
	defer unlock()
	if err != nil {
		return nil, err
	}
	var users []*models.User
	for _, u := range s.users {
		if u.IMDbSync == nil || u.IMDbSync.SourceID == "" {
			continue
		}
		if u.IMDbAttemptAt != nil && u.IMDbAttemptAt.After(ms(attemptedBefore)) {
			continue
		}
		users = append(users, cloneUser(u))
	}
	// Longest wait first; a user never fetched sorts before all others.
	sort.Slice(users, func(i, j int) bool {
		a, b := users[i].IMDbAttemptAt, users[j].IMDbAttemptAt
		switch {
		case a == nil && b == nil, a != nil && b != nil && a.Equal(*b):
			return users[i].ID.Hex() < users[j].ID.Hex()
		case a == nil || b == nil:
			return a == nil
		default:
			return a.Before(*b)
		}
	})
	return users, nil
}

// --- watchlist ---

func (s *Store) CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error {
	unlock, err := s.begin(ctx, "CreateWatchlistItem")
	defer unlock()
	if err != nil {
		return err
	}
	if s.duplicateOf(item) {
		return database.ErrDuplicateWatchlistItem
	}
	item.ID = primitive.NewObjectID()
	if item.AddedAt.IsZero() {
		item.AddedAt = ms(time.Now())
	}
	item.UpdatedAt = item.AddedAt
	s.items = append(s.items, cloneItem(item))
	s.Writes++
	return nil
}

// CreateWatchlistItems inserts the items that are not duplicates and returns
// them, stamped a millisecond apart like the real insert.
func (s *Store) CreateWatchlistItems(ctx context.Context, items []*models.WatchlistItem) ([]*models.WatchlistItem, error) {
	unlock, err := s.begin(ctx, "CreateWatchlistItems")
	defer unlock()
	if err != nil {
		return nil, err
	}
	now := ms(time.Now())
	var created []*models.WatchlistItem
	for i, item := range items {
		if s.duplicateOf(item) {
			continue
		}
		if item.Status == "" {
			item.Status = models.WatchlistStatusPlanned
		}
		item.ID = primitive.NewObjectID()
		item.AddedAt = now.Add(-time.Duration(len(items)-1-i) * time.Millisecond)
		item.UpdatedAt = item.AddedAt
		s.items = append(s.items, cloneItem(item))
		created = append(created, item)
		s.Writes++
	}
	return created, nil
}

// findItem returns the user's stored item, or nil.
func (s *Store) findItem(userID, itemID primitive.ObjectID) *models.WatchlistItem {
	for _, item := range s.items {
		if item.ID == itemID && item.UserID == userID {
			return item
		}
	}
	return nil
}

// UpdateWatchlistItem sets fields, keyed by their stored names, on the user's
// item. Like the real update it matches on the item and the user together,
// so another user's item is not found, and it refuses a change that would
// make the item a duplicate of another one.
func (s *Store) UpdateWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error) {
	unlock, err := s.begin(ctx, "UpdateWatchlistItem")
	defer unlock()
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, errors.New("no fields to update")
	}
	item := s.findItem(userID, itemID)
	if item == nil {
		return nil, database.ErrWatchlistItemNotFound
	}
	updated := cloneItem(item)
	for field, value := range fields {
		switch field {
		case "title":
			updated.Title, _ = value.(string)
		case "status":
			updated.Status, _ = value.(string)
		case "tags":
			tags, _ := value.([]string)
			updated.Tags = append([]string(nil), tags...)
		case "year":
			updated.Year, _ = value.(int)
		case "tmdb_id":
			updated.TMDbID, _ = value.(int)
		default:
			return nil, errors.New("storetest: update of " + field + " is not implemented")
		}
	}
	for _, other := range s.items {
		if other != item && other.UserID == userID && updated.TMDbID > 0 &&
			other.TMDbID == updated.TMDbID && other.Type == updated.Type {
			return nil, database.ErrDuplicateWatchlistItem
		}
	}
	updated.UpdatedAt = ms(time.Now())
	*item = *updated
	s.Writes++
	return cloneItem(item), nil
}

// DeleteWatchlistItem removes the user's item. Like the real delete it
// matches on the item and the user together.
func (s *Store) DeleteWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID) error {
	unlock, err := s.begin(ctx, "DeleteWatchlistItem")
	defer unlock()
	if err != nil {
		return err
	}
	for i, item := range s.items {
		if item.ID == itemID && item.UserID == userID {
			s.items = append(s.items[:i], s.items[i+1:]...)
			s.Writes++
			return nil
		}
	}
	return database.ErrWatchlistItemNotFound
}

// AdoptWatchlistItem applies the same conditions as the real filter: the
// IMDb ID it was matched by, no source or "csv", still planned, untagged.
func (s *Store) AdoptWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, imdbID string) (bool, error) {
	unlock, err := s.begin(ctx, "AdoptWatchlistItem")
	defer unlock()
	if err != nil {
		return false, err
	}
	item := s.findItem(userID, itemID)
	if item == nil || imdbID == "" || item.IMDbID != imdbID || !item.Adoptable() {
		return false, nil
	}
	item.Source = models.SourceIMDb
	s.Writes++
	return true, nil
}

// MarkWatchlistItemIMDbMissing applies the same conditions as the real
// filter: sync-added, still planned, untagged and not marked yet.
func (s *Store) MarkWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID, since time.Time) (bool, error) {
	unlock, err := s.begin(ctx, "MarkWatchlistItemIMDbMissing")
	defer unlock()
	if err != nil {
		return false, err
	}
	item := s.findItem(userID, itemID)
	if item == nil || item.Source != models.SourceIMDb || item.Status != models.WatchlistStatusPlanned ||
		len(item.Tags) > 0 || item.IMDbMissingSince != nil {
		return false, nil
	}
	item.IMDbMissingSince = msPtr(&since)
	s.Writes++
	return true, nil
}

func (s *Store) ClearWatchlistItemIMDbMissing(ctx context.Context, userID, itemID primitive.ObjectID) (bool, error) {
	unlock, err := s.begin(ctx, "ClearWatchlistItemIMDbMissing")
	defer unlock()
	if err != nil {
		return false, err
	}
	item := s.findItem(userID, itemID)
	if item == nil || item.IMDbMissingSince == nil {
		return false, nil
	}
	item.IMDbMissingSince = nil
	s.Writes++
	return true, nil
}

func (s *Store) ClearIMDbMissingMarks(ctx context.Context, userID primitive.ObjectID) (int, error) {
	unlock, err := s.begin(ctx, "ClearIMDbMissingMarks")
	defer unlock()
	if err != nil {
		return 0, err
	}
	cleared := 0
	for _, item := range s.items {
		if item.UserID == userID && item.IMDbMissingSince != nil {
			item.IMDbMissingSince = nil
			cleared++
			s.Writes++
		}
	}
	return cleared, nil
}

// DeleteSyncedWatchlistItem applies the same conditions as the real delete
// filter: sync-added, still planned, untagged, marked missing at exactly
// missingSince, and not edited since updatedAt.
func (s *Store) DeleteSyncedWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, missingSince, updatedAt time.Time) (bool, error) {
	unlock, err := s.begin(ctx, "DeleteSyncedWatchlistItem")
	defer unlock()
	if err != nil {
		return false, err
	}
	for i, item := range s.items {
		if item.ID != itemID || item.UserID != userID {
			continue
		}
		if item.Source != models.SourceIMDb || item.Status != models.WatchlistStatusPlanned || len(item.Tags) > 0 ||
			item.IMDbMissingSince == nil || !item.IMDbMissingSince.Equal(ms(missingSince)) ||
			!ms(item.UpdatedAt).Equal(ms(updatedAt)) {
			return false, nil
		}
		s.items = append(s.items[:i], s.items[i+1:]...)
		s.Writes++
		return true, nil
	}
	return false, nil
}

// ListWatchlistItems returns the user's items, newest first. Like the real
// query it honours status, type, limit and offset; search is not needed by
// any caller and is rejected.
func (s *Store) ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error) {
	unlock, err := s.begin(ctx, "ListWatchlistItems")
	defer unlock()
	if err != nil {
		return nil, err
	}
	if opts.Search != "" {
		return nil, errors.New("storetest: search is not implemented")
	}
	var out []*models.WatchlistItem
	for _, item := range s.items {
		if item.UserID != opts.UserID ||
			(opts.Status != "" && item.Status != opts.Status) ||
			(opts.Type != "" && item.Type != opts.Type) {
			continue
		}
		cp := *item
		out = append(out, &cp)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].AddedAt.After(out[j].AddedAt) })
	if opts.Offset > 0 {
		if opts.Offset >= len(out) {
			return nil, nil
		}
		out = out[opts.Offset:]
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

func (s *Store) DistinctWatchlistTitles(ctx context.Context) ([]models.TitleRef, error) {
	unlock, err := s.begin(ctx, "DistinctWatchlistTitles")
	defer unlock()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var refs []models.TitleRef
	for _, item := range s.items {
		ref, ok := item.TitleRef()
		if !ok || seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key() < refs[j].Key() })
	return refs, nil
}

func (s *Store) GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error) {
	unlock, err := s.begin(ctx, "GetTitles")
	defer unlock()
	if err != nil {
		return nil, err
	}
	if len(regions) == 0 {
		regions = nil
	}
	out := make(map[string]*models.Title, len(keys))
	for _, key := range keys {
		if t, ok := s.titles[key]; ok {
			out[key] = cloneTitle(t, regions)
		}
	}
	return out, nil
}

func (s *Store) GetTitlesByIMDbIDs(ctx context.Context, imdbIDs []string) (map[string]*models.Title, error) {
	unlock, err := s.begin(ctx, "GetTitlesByIMDbIDs")
	defer unlock()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(s.titles))
	for key := range s.titles {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]*models.Title, len(imdbIDs))
	for _, key := range keys {
		t := s.titles[key]
		if t.IMDbID == "" || t.NotFoundAt != nil || !contains(imdbIDs, t.IMDbID) {
			continue
		}
		if _, taken := out[t.IMDbID]; !taken {
			out[t.IMDbID] = cloneTitle(t, []string{}) // no provider snapshots
		}
	}
	return out, nil
}

func (s *Store) SaveTitleSnapshot(ctx context.Context, ref models.TitleRef, region string, prevVersion *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error) {
	unlock, err := s.begin(ctx, "SaveTitleSnapshot")
	defer unlock()
	if err != nil {
		return false, err
	}
	region, ok := models.NormalizeRegion(region)
	if !ok {
		return false, errors.New("invalid region")
	}
	now := ms(time.Now())
	t, exists := s.titles[ref.Key()]
	current, hasSnapshot := t.Snapshot(region)
	switch {
	case prevVersion == nil && hasSnapshot:
		return false, nil
	case prevVersion != nil && (!hasSnapshot || !current.FetchedAt.Equal(ms(*prevVersion))):
		return false, nil
	}
	if !exists {
		t = &models.Title{ID: ref.Key(), CreatedAt: now, Providers: map[string]models.RegionSnapshot{}}
		s.titles[ref.Key()] = t
	}
	if t.Providers == nil {
		t.Providers = map[string]models.RegionSnapshot{}
	}
	snap.FetchedAt = ms(snap.FetchedAt)
	snap.Offers = append([]models.ProviderOffer(nil), snap.Offers...)
	t.Providers[region] = snap
	t.MediaType, t.TMDbID, t.UpdatedAt, t.NotFoundAt = ref.MediaType, ref.TMDbID, now, nil
	if meta != nil {
		t.TitleMetadata = *meta
		t.MetadataFetchedAt = msPtr(meta.MetadataFetchedAt)
	}
	s.Writes++
	return true, nil
}

func (s *Store) MarkTitleNotFound(ctx context.Context, ref models.TitleRef, at time.Time) error {
	unlock, err := s.begin(ctx, "MarkTitleNotFound")
	defer unlock()
	if err != nil {
		return err
	}
	t, ok := s.titles[ref.Key()]
	if !ok {
		t = &models.Title{ID: ref.Key(), MediaType: ref.MediaType, TMDbID: ref.TMDbID, CreatedAt: ms(at)}
		s.titles[ref.Key()] = t
	}
	t.NotFoundAt, t.UpdatedAt = msPtr(&at), ms(at)
	s.Writes++
	return nil
}

func (s *Store) InsertAvailabilityEvents(ctx context.Context, events []models.AvailabilityEvent) (int, error) {
	unlock, err := s.begin(ctx, "InsertAvailabilityEvents")
	defer unlock()
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, ev := range events {
		if _, dup := s.events[ev.ID]; dup {
			continue
		}
		ev.DetectedAt, ev.ExpiresAt = ms(ev.DetectedAt), ms(ev.ExpiresAt)
		s.events[ev.ID] = ev
		inserted++
		s.Writes++
	}
	return inserted, nil
}

func (s *Store) ListAvailabilityEvents(ctx context.Context, titleKeys []string, region string, since time.Time) ([]models.AvailabilityEvent, error) {
	unlock, err := s.begin(ctx, "ListAvailabilityEvents")
	defer unlock()
	if err != nil {
		return nil, err
	}
	var out []models.AvailabilityEvent
	for _, ev := range s.events {
		if ev.Region == region && contains(titleKeys, ev.TitleKey) && !ev.DetectedAt.Before(since) {
			out = append(out, ev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DetectedAt.Before(out[j].DetectedAt) })
	return out, nil
}

func (s *Store) InsertDigestDeliveries(ctx context.Context, deliveries []models.DigestDelivery) (int, error) {
	unlock, err := s.begin(ctx, "InsertDigestDeliveries")
	defer unlock()
	if err != nil {
		return 0, err
	}
	inserted := 0
	for _, d := range deliveries {
		key := deliveryKey{d.UserID, d.EventID}
		if _, dup := s.deliveries[key]; dup {
			continue
		}
		d.SentAt, d.ExpiresAt = ms(d.SentAt), ms(d.ExpiresAt)
		s.deliveries[key] = d
		inserted++
		s.Writes++
	}
	return inserted, nil
}

func (s *Store) ListDeliveredEventIDs(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (map[string]bool, error) {
	unlock, err := s.begin(ctx, "ListDeliveredEventIDs")
	defer unlock()
	if err != nil {
		return nil, err
	}
	delivered := make(map[string]bool)
	for _, id := range eventIDs {
		if _, ok := s.deliveries[deliveryKey{userID, id}]; ok {
			delivered[id] = true
		}
	}
	return delivered, nil
}

func (s *Store) DeleteDigestDeliveries(ctx context.Context, userID primitive.ObjectID, eventIDs []string) (int, error) {
	unlock, err := s.begin(ctx, "DeleteDigestDeliveries")
	defer unlock()
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, id := range eventIDs {
		key := deliveryKey{userID, id}
		if _, ok := s.deliveries[key]; ok {
			delete(s.deliveries, key)
			deleted++
			s.Writes++
		}
	}
	return deleted, nil
}
