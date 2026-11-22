package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/database"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/server"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	demoName   = "Demo"
	demoRegion = "US"
)

// demoServices are the services the demo account has, all active.
var demoServices = []string{
	models.ServiceNetflix,
	models.ServiceMax,
	models.ServiceHulu,
	models.ServiceDisneyPlus,
	models.ServicePrimeVideo,
	models.ServiceAppleTVPlus,
}

//go:embed titles.csv
var titlesCSV []byte

// seedTitle is one row of titles.csv.
type seedTitle struct {
	Type   string // movie | show
	TMDbID int
	IMDbID string
	Year   int
	Title  string
	Status string
}

func (t seedTitle) ref() models.TitleRef {
	mediaType, _ := models.TMDbMediaType(t.Type)
	return models.TitleRef{MediaType: mediaType, TMDbID: t.TMDbID}
}

// parseTitles reads the embedded list: type,tmdb_id,imdb_id,year,title,status
// under a header row.
func parseTitles(data []byte) ([]seedTitle, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = 6
	var titles []seedTitle
	seen := make(map[string]bool)
	for line := 1; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("titles.csv: %w", err)
		}
		if line == 1 {
			continue // header
		}
		id, err := strconv.Atoi(rec[1])
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("titles.csv line %d: invalid tmdb_id %q", line, rec[1])
		}
		year, err := strconv.Atoi(rec[3])
		if err != nil {
			return nil, fmt.Errorf("titles.csv line %d: invalid year %q", line, rec[3])
		}
		t := seedTitle{Type: rec[0], TMDbID: id, IMDbID: rec[2], Year: year, Title: strings.TrimSpace(rec[4]), Status: rec[5]}
		if _, ok := models.TMDbMediaType(t.Type); !ok {
			return nil, fmt.Errorf("titles.csv line %d: invalid type %q", line, t.Type)
		}
		switch t.Status {
		case models.WatchlistStatusPlanned, models.WatchlistStatusWatching, models.WatchlistStatusFinished:
		default:
			return nil, fmt.Errorf("titles.csv line %d: invalid status %q", line, t.Status)
		}
		if t.Title == "" || !strings.HasPrefix(t.IMDbID, "tt") {
			return nil, fmt.Errorf("titles.csv line %d: missing title or IMDb ID", line)
		}
		if key := t.ref().Key(); seen[key] {
			return nil, fmt.Errorf("titles.csv line %d: duplicate title %s", line, key)
		} else {
			seen[key] = true
		}
		titles = append(titles, t)
	}
	if len(titles) == 0 {
		return nil, errors.New("titles.csv: no titles")
	}
	return titles, nil
}

// addedAt spreads the list over the months before now, so that "recently
// added" shows a mix of films and shows instead of the file's order. The
// order depends only on the titles, so every run agrees on it.
func addedAt(titles []seedTitle, now time.Time) map[string]time.Time {
	type ranked struct {
		key  string
		hash uint32
	}
	order := make([]ranked, len(titles))
	for i, t := range titles {
		h := fnv.New32a()
		_, _ = h.Write([]byte(t.ref().Key()))
		order[i] = ranked{key: t.ref().Key(), hash: h.Sum32()}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].hash != order[j].hash {
			return order[i].hash < order[j].hash
		}
		return order[i].key < order[j].key
	})

	const step = 31 * time.Hour
	times := make(map[string]time.Time, len(order))
	for rank, entry := range order {
		// Up to 19 hours of jitter keeps the times from looking machine-made.
		jitter := time.Duration(entry.hash%(19*60)) * time.Minute
		times[entry.key] = now.Add(-time.Duration(rank+1)*step - jitter).UTC().Truncate(time.Second)
	}
	return times
}

// store is the persistence the seeder uses. Every write is addressed to the
// demo account's own ID, or to its address when the account is created.
type store interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpsertUser(ctx context.Context, u *models.User) error
	UpdateServices(ctx context.Context, userID primitive.ObjectID, services []models.ServiceSubscription) (*models.User, error)
	UpdateUserPreferences(ctx context.Context, userID primitive.ObjectID, fields map[string]any) (*models.User, error)
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	CreateWatchlistItem(ctx context.Context, item *models.WatchlistItem) error
	UpdateWatchlistItem(ctx context.Context, userID, itemID primitive.ObjectID, fields map[string]any) (*models.WatchlistItem, error)
	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
}

// ensurer stores availability baselines for titles that have none. The
// availability refresher is one.
type ensurer interface {
	Ensure(ctx context.Context, refs []models.TitleRef, stored map[string]*models.Title, region string) map[string]*models.Title
}

// report counts what a run did, or in a dry run what it would do.
type report struct {
	DryRun bool
	Titles int // titles in the embedded list

	UserCreated      bool
	ProfileUpdated   bool // name or region corrected
	ServicesSet      bool
	PreferencesReset bool

	ItemsInserted int
	ItemsExisting int

	TitlesStored     int // already had availability for the region
	TitlesFetched    int // baseline stored by this run
	TitlesUnresolved int // could not be fetched; a re-run retries them
}

// seed brings the demo account to its intended state and reports what it
// changed. It only ever adds what is missing: existing items keep their
// status and dates, and items that are not in the list are left alone, so
// running it twice changes nothing the second time.
//
// With dryRun it reads the same things and writes nothing.
func seed(ctx context.Context, db store, fetch ensurer, titles []seedTitle, now time.Time, dryRun bool) (report, error) {
	rep := report{DryRun: dryRun, Titles: len(titles)}

	user, err := db.GetUserByEmail(ctx, server.DemoUserEmail)
	if err != nil {
		return rep, fmt.Errorf("load demo user: %w", err)
	}
	rep.UserCreated = user == nil
	rep.ProfileUpdated = user != nil && (user.Name != demoName || user.Region != demoRegion)
	if (rep.UserCreated || rep.ProfileUpdated) && !dryRun {
		if err := db.UpsertUser(ctx, &models.User{Email: server.DemoUserEmail, Name: demoName, Region: demoRegion}); err != nil {
			return rep, fmt.Errorf("upsert demo user: %w", err)
		}
		if user, err = db.GetUserByEmail(ctx, server.DemoUserEmail); err != nil || user == nil {
			return rep, fmt.Errorf("reload demo user: %w", errors.Join(err, errMissingAfterUpsert(user)))
		}
	}

	rep.ServicesSet = user == nil || !hasExactlyServices(user, demoServices)
	rep.PreferencesReset = user == nil || !quietPreferences(user.Preferences)
	if !dryRun {
		if rep.ServicesSet {
			if _, err := db.UpdateServices(ctx, user.ID, wantedServices(user)); err != nil {
				return rep, fmt.Errorf("set services: %w", err)
			}
		}
		// A freshly created account already has these values; writing them
		// again makes sure of it whatever the defaults become.
		if rep.PreferencesReset {
			if _, err := db.UpdateUserPreferences(ctx, user.ID, map[string]any{
				"preferences.digest.enabled":    false,
				"preferences.digest_consent":    false,
				"preferences.marketing_consent": false,
				"preferences.use_account_email": true,
				"preferences.notify_email":      "",
			}); err != nil {
				return rep, fmt.Errorf("reset preferences: %w", err)
			}
		}
	}

	existing := make(map[string]bool)
	if user != nil {
		items, err := db.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user.ID})
		if err != nil {
			return rep, fmt.Errorf("list demo items: %w", err)
		}
		for _, item := range items {
			if ref, ok := item.TitleRef(); ok {
				existing[ref.Key()] = true
			}
		}
	}
	times := addedAt(titles, now)
	for _, t := range titles {
		key := t.ref().Key()
		if existing[key] {
			rep.ItemsExisting++
			continue
		}
		if dryRun {
			rep.ItemsInserted++
			continue
		}
		item := &models.WatchlistItem{
			UserID: user.ID, Title: t.Title, Type: t.Type, Year: t.Year,
			TMDbID: t.TMDbID, IMDbID: t.IMDbID, Status: t.Status,
		}
		if err := db.CreateWatchlistItem(ctx, item); err != nil {
			if errors.Is(err, database.ErrDuplicateWatchlistItem) {
				rep.ItemsExisting++ // another run got there first
				continue
			}
			return rep, fmt.Errorf("insert %s: %w", key, err)
		}
		rep.ItemsInserted++
		// Creating an item stamps it with the current time; move it back.
		if _, err := db.UpdateWatchlistItem(ctx, user.ID, item.ID, map[string]any{"added_at": times[key]}); err != nil {
			return rep, fmt.Errorf("date %s: %w", key, err)
		}
	}

	refs := make([]models.TitleRef, len(titles))
	keys := make([]string, len(titles))
	for i, t := range titles {
		refs[i] = t.ref()
		keys[i] = refs[i].Key()
	}
	stored, err := db.GetTitles(ctx, keys, []string{demoRegion})
	if err != nil {
		return rep, fmt.Errorf("load stored titles: %w", err)
	}
	for _, key := range keys {
		if _, ok := stored[key].Snapshot(demoRegion); ok {
			rep.TitlesStored++
		}
	}
	if dryRun {
		rep.TitlesFetched = len(keys) - rep.TitlesStored
		return rep, nil
	}
	resolved := fetch.Ensure(ctx, refs, stored, demoRegion)
	rep.TitlesFetched = len(resolved) - rep.TitlesStored
	rep.TitlesUnresolved = len(keys) - len(resolved)
	return rep, nil
}

func errMissingAfterUpsert(user *models.User) error {
	if user == nil {
		return errors.New("the account was not found after it was written")
	}
	return nil
}

// hasExactlyServices reports whether the user's active services are exactly
// codes and nothing is stored inactive.
func hasExactlyServices(user *models.User, codes []string) bool {
	if len(user.Services) != len(codes) {
		return false
	}
	active := user.ActiveServiceCodes()
	for _, code := range codes {
		if !active[code] {
			return false
		}
	}
	return true
}

// wantedServices builds the demo account's service list, keeping the date of
// any service it already has.
func wantedServices(user *models.User) []models.ServiceSubscription {
	since := make(map[string]time.Time, len(user.Services))
	for _, svc := range user.Services {
		since[strings.ToLower(svc.Code)] = svc.AddedAt
	}
	out := make([]models.ServiceSubscription, len(demoServices))
	for i, code := range demoServices {
		out[i] = models.ServiceSubscription{Code: code, Active: true, AddedAt: since[code]}
	}
	return out
}

// quietPreferences reports whether the account can never be emailed: digests
// off, no consents, no separate notification address.
func quietPreferences(p models.Preferences) bool {
	return !p.Digest.Enabled && !p.DigestConsent && !p.MarketingConsent && p.UseAccountEmail && p.NotifyEmail == ""
}

// String renders the report as the command's output: counts, no titles and
// no addresses.
func (r report) String() string {
	var b strings.Builder
	verb := func(did, would string) string {
		if r.DryRun {
			return would
		}
		return did
	}
	if r.DryRun {
		b.WriteString("dry run: nothing was written\n")
	}
	switch {
	case r.UserCreated:
		fmt.Fprintf(&b, "demo account: %s\n", verb("created", "would be created"))
	case r.ProfileUpdated || r.ServicesSet || r.PreferencesReset:
		fmt.Fprintf(&b, "demo account: %s\n", verb("updated", "would be updated"))
	default:
		b.WriteString("demo account: up to date\n")
	}
	fmt.Fprintf(&b, "titles in the list: %d\n", r.Titles)
	fmt.Fprintf(&b, "watchlist items: %d %s, %d already there\n", r.ItemsInserted, verb("inserted", "to insert"), r.ItemsExisting)
	fmt.Fprintf(&b, "availability: %d already stored, %d %s", r.TitlesStored, r.TitlesFetched, verb("fetched", "to fetch"))
	if r.TitlesUnresolved > 0 {
		fmt.Fprintf(&b, ", %d could not be fetched (run again to retry)", r.TitlesUnresolved)
	}
	b.WriteString("\n")
	return b.String()
}
