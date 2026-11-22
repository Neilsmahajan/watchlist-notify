// Package storetest provides an in-memory stand-in for the MongoDB-backed
// database.Service, for unit tests that must run without a database. It
// reproduces the semantics the application relies on: compare-and-set on
// snapshots and digest schedules, event de-duplication by ID, per-region
// projection, and millisecond time precision.
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
	}
}

// begin locks the store, counts the call and reports the injected error.
func (s *Store) begin(method string) (unlock func(), err error) {
	s.mu.Lock()
	s.Queries[method]++
	return s.mu.Unlock, s.Err
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
	cp := *s.users[id]
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

func (s *Store) Health(context.Context) error {
	unlock, err := s.begin("Health")
	defer unlock()
	return err
}

func (s *Store) GetUserByEmail(_ context.Context, email string) (*models.User, error) {
	unlock, err := s.begin("GetUserByEmail")
	defer unlock()
	if err != nil {
		return nil, err
	}
	for _, u := range s.users {
		if strings.EqualFold(u.Email, email) {
			cp := *u
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *Store) GetUsersForDigest(_ context.Context, now time.Time) ([]*models.User, error) {
	unlock, err := s.begin("GetUsersForDigest")
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

func (s *Store) CompareAndSwapDigestSchedule(_ context.Context, userID primitive.ObjectID, from, to models.DigestSchedule, requireEnabled bool) (bool, error) {
	unlock, err := s.begin("CompareAndSwapDigestSchedule")
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

func (s *Store) DistinctUserRegions(context.Context) ([]string, error) {
	unlock, err := s.begin("DistinctUserRegions")
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

func (s *Store) CreateWatchlistItem(_ context.Context, item *models.WatchlistItem) error {
	unlock, err := s.begin("CreateWatchlistItem")
	defer unlock()
	if err != nil {
		return err
	}
	for _, existing := range s.items {
		if existing.UserID == item.UserID && item.TMDbID > 0 && existing.TMDbID == item.TMDbID && existing.Type == item.Type {
			return database.ErrDuplicateWatchlistItem
		}
	}
	item.ID = primitive.NewObjectID()
	if item.AddedAt.IsZero() {
		item.AddedAt = ms(time.Now())
	}
	item.UpdatedAt = item.AddedAt
	cp := *item
	s.items = append(s.items, &cp)
	s.Writes++
	return nil
}

// ListWatchlistItems returns the user's items, newest first. Like the real
// query it honours status, type, limit and offset; search is not needed by
// any caller and is rejected.
func (s *Store) ListWatchlistItems(_ context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error) {
	unlock, err := s.begin("ListWatchlistItems")
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

func (s *Store) DistinctWatchlistTitles(context.Context) ([]models.TitleRef, error) {
	unlock, err := s.begin("DistinctWatchlistTitles")
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

func (s *Store) GetTitles(_ context.Context, keys []string, regions []string) (map[string]*models.Title, error) {
	unlock, err := s.begin("GetTitles")
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

func (s *Store) SaveTitleSnapshot(_ context.Context, ref models.TitleRef, region string, prevVersion *time.Time, snap models.RegionSnapshot, meta *models.TitleMetadata) (bool, error) {
	unlock, err := s.begin("SaveTitleSnapshot")
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

func (s *Store) MarkTitleNotFound(_ context.Context, ref models.TitleRef, at time.Time) error {
	unlock, err := s.begin("MarkTitleNotFound")
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

func (s *Store) InsertAvailabilityEvents(_ context.Context, events []models.AvailabilityEvent) (int, error) {
	unlock, err := s.begin("InsertAvailabilityEvents")
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

func (s *Store) ListAvailabilityEvents(_ context.Context, titleKeys []string, region string, since time.Time) ([]models.AvailabilityEvent, error) {
	unlock, err := s.begin("ListAvailabilityEvents")
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

func (s *Store) InsertDigestDeliveries(_ context.Context, deliveries []models.DigestDelivery) (int, error) {
	unlock, err := s.begin("InsertDigestDeliveries")
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

func (s *Store) ListDeliveredEventIDs(_ context.Context, userID primitive.ObjectID, eventIDs []string) (map[string]bool, error) {
	unlock, err := s.begin("ListDeliveredEventIDs")
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

func (s *Store) DeleteDigestDeliveries(_ context.Context, userID primitive.ObjectID, eventIDs []string) (int, error) {
	unlock, err := s.begin("DeleteDigestDeliveries")
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
