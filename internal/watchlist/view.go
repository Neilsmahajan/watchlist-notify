package watchlist

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// MissBudget is how long a list request may spend fetching titles that have
// no stored availability yet. After the first backfill a miss is rare (an
// item added while TMDb was unreachable), so this is a ceiling, not a cost.
const MissBudget = 2 * time.Second

// Store is the persistence a list needs: two queries, whatever the list size.
type Store interface {
	ListWatchlistItems(ctx context.Context, opts models.ListWatchlistOptions) ([]*models.WatchlistItem, error)
	GetTitles(ctx context.Context, keys []string, regions []string) (map[string]*models.Title, error)
}

// Ensurer fetches and stores titles that have no snapshot for a region yet.
// It is best effort: titles it could not resolve are absent from the result.
type Ensurer interface {
	Ensure(ctx context.Context, refs []models.TitleRef, stored map[string]*models.Title, region string) map[string]*models.Title
}

// Options selects, orders and pages a list. The zero value lists everything,
// newest first.
type Options struct {
	Status string // planned | watching | finished
	Type   string // movie | show
	Search string // case-insensitive substring of the title
	// Services keeps items streaming on at least one of these service codes.
	Services []string
	// Available keeps items that are (true) or are not (false) streaming on
	// one of the user's active services. Nil does not filter.
	Available *bool
	// Sort is a field name, optionally prefixed with "-" for descending:
	// added_at, title, year, status, rating, runtime, available. Anything
	// else sorts by -added_at.
	Sort   string
	Limit  int // 0 means no limit
	Offset int
}

// Item is a watchlist item with its title metadata and availability attached.
// The embedded item keeps every field the API returned before availability
// was stored, so older clients read it unchanged.
type Item struct {
	*models.WatchlistItem
	PosterPath string   `json:"poster_path,omitempty"`
	Runtime    int      `json:"runtime,omitempty"`
	Genres     []string `json:"genres,omitempty"`
	Rating     float64  `json:"rating,omitempty"`
	Overview   string   `json:"overview,omitempty"`
	// Availability is null when the title has no stored snapshot: it is not
	// linked to TMDb, TMDb no longer has it, or the fetch has not succeeded yet.
	Availability *Availability `json:"availability"`

	services map[string]bool // every service code streaming the title
}

// available reports whether one of the user's services streams the item.
func (it *Item) available() bool {
	return it.Availability != nil && len(it.Availability.Providers) > 0
}

// Facets count the items behind each filter value, so a client can label its
// filters without further requests.
type Facets struct {
	// Services counts items streaming on each service code, for every
	// service that streams at least one.
	Services map[string]int `json:"services"`
	// Available and Unavailable split the same items by whether one of the
	// user's active services streams them.
	Available   int `json:"available"`
	Unavailable int `json:"unavailable"`
	// Status counts items per status.
	Status map[string]int `json:"status"`
}

// Timings says where a list spent its time.
type Timings struct {
	DB   time.Duration // the items query and the titles query
	TMDb time.Duration // fetching titles that had no stored snapshot
}

// View is the result of a list.
type View struct {
	Items []Item
	// Total is the number of items matching the filters, before paging.
	Total   int
	Facets  Facets
	Region  string
	Timings Timings
}

// Lister builds watchlist views.
type Lister struct {
	store         Store
	ensurer       Ensurer // may be nil: misses then stay unresolved
	defaultRegion string
	missBudget    time.Duration
}

// NewLister returns a Lister. ensurer may be nil.
func NewLister(store Store, ensurer Ensurer, defaultRegion string) *Lister {
	return &Lister{store: store, ensurer: ensurer, defaultRegion: defaultRegion, missBudget: MissBudget}
}

// List returns the user's watchlist as opts describes it.
//
// It always loads the whole list (one query) and the titles behind it (a
// second query) and does the rest in memory. Lists are small, a few hundred
// items at most, and filtering on availability, counting facets and sorting
// by rating all need the joined rows anyway.
//
// Facets describe the list around the current selection rather than inside
// it: the status counts ignore the status filter (so the other tabs do not
// all read zero), and the service and availability counts ignore the service
// and availability filters. Type and search apply to all of them.
func (l *Lister) List(ctx context.Context, user *models.User, opts Options) (*View, error) {
	region := user.RegionOr(l.defaultRegion)
	view := &View{Region: region}

	dbStart := time.Now()
	items, err := l.store.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user.ID})
	if err != nil {
		return nil, err
	}
	var refs []models.TitleRef
	var keys []string
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		ref, ok := item.TitleRef()
		if !ok || seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		refs = append(refs, ref)
		keys = append(keys, ref.Key())
	}
	titles, err := l.store.GetTitles(ctx, keys, []string{region})
	if err != nil {
		return nil, err
	}
	if titles == nil {
		titles = make(map[string]*models.Title)
	}
	view.Timings.DB = time.Since(dbStart)

	if missing := withoutSnapshot(refs, titles, region); len(missing) > 0 && l.ensurer != nil {
		tmdbStart := time.Now()
		fetchCtx, cancel := context.WithTimeout(ctx, l.missBudget)
		for key, title := range l.ensurer.Ensure(fetchCtx, missing, titles, region) {
			titles[key] = title
		}
		cancel()
		view.Timings.TMDb = time.Since(tmdbStart)
	}

	active := user.ActiveServiceCodes()
	rows := make([]Item, 0, len(items))
	for _, item := range items {
		rows = append(rows, join(item, titles, region, active))
	}

	// Type and search narrow everything, facets included.
	search := strings.ToLower(strings.TrimSpace(opts.Search))
	rows = filter(rows, func(it *Item) bool {
		if opts.Type != "" && it.Type != opts.Type {
			return false
		}
		return search == "" || strings.Contains(strings.ToLower(it.Title), search)
	})

	view.Facets = Facets{Services: map[string]int{}, Status: map[string]int{}}
	for i := range rows {
		view.Facets.Status[rows[i].Status]++
	}
	if opts.Status != "" {
		rows = filter(rows, func(it *Item) bool { return it.Status == opts.Status })
	}
	for i := range rows {
		for code := range rows[i].services {
			view.Facets.Services[code]++
		}
		if rows[i].available() {
			view.Facets.Available++
		} else {
			view.Facets.Unavailable++
		}
	}

	if len(opts.Services) > 0 {
		rows = filter(rows, func(it *Item) bool {
			for _, code := range opts.Services {
				if it.services[code] {
					return true
				}
			}
			return false
		})
	}
	if opts.Available != nil {
		want := *opts.Available
		rows = filter(rows, func(it *Item) bool { return it.available() == want })
	}

	sortItems(rows, opts.Sort)
	view.Total = len(rows)

	if opts.Offset > 0 {
		if opts.Offset >= len(rows) {
			rows = rows[:0]
		} else {
			rows = rows[opts.Offset:]
		}
	}
	if opts.Limit > 0 && len(rows) > opts.Limit {
		rows = rows[:opts.Limit]
	}
	view.Items = rows
	return view, nil
}

// withoutSnapshot returns the refs that have no snapshot for region.
func withoutSnapshot(refs []models.TitleRef, titles map[string]*models.Title, region string) []models.TitleRef {
	var missing []models.TitleRef
	for _, ref := range refs {
		if _, ok := titles[ref.Key()].Snapshot(region); !ok {
			missing = append(missing, ref)
		}
	}
	return missing
}

// join attaches the stored title to an item.
func join(item *models.WatchlistItem, titles map[string]*models.Title, region string, active map[string]bool) Item {
	row := Item{WatchlistItem: item}
	ref, ok := item.TitleRef()
	if !ok {
		return row
	}
	title := titles[ref.Key()]
	if title == nil {
		return row
	}
	row.PosterPath = title.PosterPath
	row.Runtime = title.Runtime
	row.Genres = title.Genres
	row.Rating = title.Rating
	row.Overview = title.Overview

	snap, ok := title.Snapshot(region)
	if !ok {
		return row
	}
	row.Availability = BuildAvailability(snap, region, active)
	row.services = make(map[string]bool)
	for _, svc := range snap.Services() {
		row.services[svc.Code] = true
	}
	return row
}

// filter keeps the rows keep accepts, reusing the slice.
func filter(rows []Item, keep func(*Item) bool) []Item {
	kept := rows[:0]
	for i := range rows {
		if keep(&rows[i]) {
			kept = append(kept, rows[i])
		}
	}
	return kept
}

// sortItems orders rows by a sort key. Ties, and unknown keys, fall back to
// newest first, so the order is stable from one request to the next.
func sortItems(rows []Item, key string) {
	field, desc := strings.TrimPrefix(key, "-"), strings.HasPrefix(key, "-")

	// cmp reports the order of a and b in ascending terms; missing says a
	// row has no value for the field and belongs at the end either way.
	var cmp func(a, b *Item) int
	var missing func(*Item) bool
	switch field {
	case "title":
		cmp = func(a, b *Item) int { return strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)) }
	case "year":
		cmp = func(a, b *Item) int { return a.Year - b.Year }
		missing = func(it *Item) bool { return it.Year == 0 }
	case "status":
		cmp = func(a, b *Item) int { return strings.Compare(a.Status, b.Status) }
	case "rating":
		cmp = func(a, b *Item) int { return compareFloat(a.Rating, b.Rating) }
		missing = func(it *Item) bool { return it.Rating == 0 }
	case "runtime":
		cmp = func(a, b *Item) int { return a.Runtime - b.Runtime }
		missing = func(it *Item) bool { return it.Runtime == 0 }
	case "available":
		// "available" puts available items first; "-available" puts them last.
		cmp = func(a, b *Item) int { return boolRank(b.available()) - boolRank(a.available()) }
	case "added_at":
		cmp = func(a, b *Item) int { return a.AddedAt.Compare(b.AddedAt) }
	default:
		cmp, desc = func(a, b *Item) int { return a.AddedAt.Compare(b.AddedAt) }, true
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := &rows[i], &rows[j]
		if missing != nil {
			if ma, mb := missing(a), missing(b); ma != mb {
				return mb
			}
		}
		c := cmp(a, b)
		if desc {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
		if !a.AddedAt.Equal(b.AddedAt) {
			return a.AddedAt.After(b.AddedAt)
		}
		return a.ID.Hex() < b.ID.Hex()
	})
}

func compareFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}
