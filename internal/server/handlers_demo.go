package server

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

// The public demo serves one seeded account's watchlist to anyone, without a
// token. Everything about it is built to be safe to leave open:
//
//   - it is read-only: there are only GET routes, and they never write;
//   - it can only ever show the demo account: the user is looked up by a
//     constant address, never by anything in the request;
//   - nobody can sign in as that account: getUser refuses its address;
//   - it never calls TMDb: the lister gets no ensurer, so a title without
//     stored availability is simply listed without it;
//   - it reads MongoDB at most once per demoCacheTTL, whatever the traffic and
//     whatever the query: the account's rows are held in memory and every
//     request is filtered, sorted and paged from that copy.

// DemoUserEmail is the reserved address of the account behind the public
// demo. cmd/seed-demo creates the account; nothing else can.
const DemoUserEmail = "demo@watchlistnotify.com"

const (
	// demoDefaultLimit matches GET /api/watchlist.
	demoDefaultLimit = defaultLimit
	// demoMaxLimit caps a page. limit=0, which means "everything" on the
	// authenticated list, means this here.
	demoMaxLimit = 60
	// demoMaxSearchLen caps the search term, in characters.
	demoMaxSearchLen = 64
	// demoCacheTTL is how long the demo account's rows are served from memory.
	demoCacheTTL = 5 * time.Minute
	// demoRetryAfter is how soon a failed or empty load is tried again. It
	// keeps a missing demo account, or a database outage, from turning every
	// anonymous request into a query.
	demoRetryAfter = 30 * time.Second
	// demoCacheControl lets browsers and CDNs keep a response as long as the
	// server would have served the same one anyway.
	demoCacheControl = "public, max-age=300"
)

// isReservedEmail reports whether email is the demo account's address. The
// comparison ignores case and surrounding space so that no spelling of the
// address gets through.
func isReservedEmail(email string) bool {
	return strings.EqualFold(strings.TrimSpace(email), DemoUserEmail)
}

// demoDB is the persistence the demo reads.
type demoDB interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	watchlist.Store
}

// demoSnapshot is the demo account as of one load. It is never modified after
// it is built, so requests share it without locking.
type demoSnapshot struct {
	user   *models.User // nil when the demo account does not exist
	items  []*models.WatchlistItem
	titles map[string]*models.Title
}

// demoSnapshot is the watchlist.Store the lister reads. Both methods hand out
// copies of the containers, so the snapshot stays intact whatever the lister
// does with them; the rows themselves are shared and only ever read.

func (s *demoSnapshot) ListWatchlistItems(context.Context, models.ListWatchlistOptions) ([]*models.WatchlistItem, error) {
	return append([]*models.WatchlistItem(nil), s.items...), nil
}

func (s *demoSnapshot) GetTitles(context.Context, []string, []string) (map[string]*models.Title, error) {
	titles := make(map[string]*models.Title, len(s.titles))
	for key, title := range s.titles {
		titles[key] = title
	}
	return titles, nil
}

// demoCache holds the current snapshot and reloads it when it is stale.
type demoCache struct {
	db            demoDB
	defaultRegion string
	now           func() time.Time

	mu      sync.Mutex
	snap    *demoSnapshot // nil until the first successful load
	err     error         // the last load's error, when there is no snapshot
	freshTo time.Time     // no load is attempted before this time
}

func newDemoCache(db demoDB, defaultRegion string) *demoCache {
	return &demoCache{db: db, defaultRegion: defaultRegion, now: time.Now}
}

// snapshot returns the demo account's rows, loading them when the copy in
// memory has expired. Concurrent callers wait for one load rather than each
// starting their own. When a reload fails, the previous snapshot keeps being
// served; with nothing to fall back on the error is returned, and remembered
// for demoRetryAfter.
func (d *demoCache) snapshot(ctx context.Context) (*demoSnapshot, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := d.now()
	if now.Before(d.freshTo) {
		return d.snap, d.err
	}

	snap, err := d.load(ctx)
	switch {
	case err != nil && d.snap != nil:
		log.Printf("demo: reload failed, serving the previous copy: %v", err)
		d.freshTo = now.Add(demoRetryAfter)
	case err != nil:
		d.err = err
		d.freshTo = now.Add(demoRetryAfter)
	case snap.user == nil:
		// Not seeded yet. Check again soon so the demo appears shortly after
		// the seeder has run.
		d.snap, d.err = snap, nil
		d.freshTo = now.Add(demoRetryAfter)
	default:
		d.snap, d.err = snap, nil
		d.freshTo = now.Add(demoCacheTTL)
	}
	return d.snap, d.err
}

// load reads the demo account, its items and their titles: three queries.
func (d *demoCache) load(ctx context.Context) (*demoSnapshot, error) {
	user, err := d.db.GetUserByEmail(ctx, DemoUserEmail)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return &demoSnapshot{}, nil
	}
	items, err := d.db.ListWatchlistItems(ctx, models.ListWatchlistOptions{UserID: user.ID})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if ref, ok := item.TitleRef(); ok && !seen[ref.Key()] {
			seen[ref.Key()] = true
			keys = append(keys, ref.Key())
		}
	}
	titles, err := d.db.GetTitles(ctx, keys, []string{user.RegionOr(d.defaultRegion)})
	if err != nil {
		return nil, err
	}
	return &demoSnapshot{user: user, items: items, titles: titles}, nil
}

// demoCaches holds one cache per Server, created on first use.
var demoCaches sync.Map // *Server -> *demoCache

func (s *Server) demoCache() *demoCache {
	if cache, ok := demoCaches.Load(s); ok {
		return cache.(*demoCache)
	}
	cache, _ := demoCaches.LoadOrStore(s, newDemoCache(s.db, s.defaultRegion))
	return cache.(*demoCache)
}

// demoAccount returns the demo snapshot, or writes the error response and
// reports false.
func (s *Server) demoAccount(c *gin.Context) (*demoSnapshot, bool) {
	snap, err := s.demoCache().snapshot(c.Request.Context())
	if err != nil {
		log.Printf("demo: load failed: %v", err)
		c.Header("Cache-Control", "no-store")
		jsonError(c, http.StatusInternalServerError, "failed to load demo")
		return nil, false
	}
	if snap.user == nil {
		c.Header("Cache-Control", "no-store")
		jsonError(c, http.StatusNotFound, "demo not available")
		return nil, false
	}
	return snap, true
}

// demoWatchlistHandler godoc
// @Summary Public demo watchlist
// @Description The watchlist of a seeded demo account, in the same shape and with the same filters, sorts and paging as GET /api/watchlist. Public and read-only. A page holds at most 60 items (limit=0 means 60), search is limited to 64 characters, and the response is served from an in-memory copy that is refreshed every five minutes, so it never triggers a TMDb lookup.
// @Tags Demo
// @Produce json
// @Param limit query int false "Number of items to return (max 60; 0 means 60)" default(20)
// @Param offset query int false "Number of items to skip" default(0)
// @Param status query string false "Filter by status" Enums(planned, watching, finished)
// @Param type query string false "Filter by content type" Enums(movie, show)
// @Param search query string false "Case-insensitive search in title (max 64 characters)"
// @Param service query []string false "Keep items streaming on any of these service codes (repeat the parameter or separate with commas)" collectionFormat(multi)
// @Param available query boolean false "Keep items that are (true) or are not (false) streaming on one of the demo account's services"
// @Param sort query string false "Sort field (prefix with - for descending): added_at, title, year, status, rating, runtime, available" default(-added_at)
// @Success 200 {object} WatchlistListResponse "Demo watchlist items with pagination metadata and facets"
// @Header 200 {string} Cache-Control "public, max-age=300"
// @Failure 400 {object} ErrorResponse "Invalid query parameters"
// @Failure 404 {object} ErrorResponse "The demo account has not been seeded"
// @Failure 500 {object} ErrorResponse "Failed to load the demo"
// @Router /api/demo/watchlist [get]
func (s *Server) demoWatchlistHandler(c *gin.Context) {
	opts, ok := parseDemoListOptions(c)
	if !ok {
		return
	}
	snap, ok := s.demoAccount(c)
	if !ok {
		return
	}

	// No ensurer: a title without stored availability stays that way.
	view, err := watchlist.NewLister(snap, nil, s.defaultRegion).List(c.Request.Context(), snap.user, opts)
	if err != nil {
		log.Printf("demo: list failed: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to load demo")
		return
	}

	meta := WatchlistListMeta{
		Limit:    opts.Limit,
		Offset:   opts.Offset,
		Returned: len(view.Items),
		Total:    view.Total,
		Region:   view.Region,
		Facets:   view.Facets,
	}
	if next := opts.Offset + len(view.Items); next < view.Total {
		meta.NextOffset = &next
	}
	c.Header("Cache-Control", demoCacheControl)
	c.JSON(http.StatusOK, WatchlistListResponse{Items: view.Items, Meta: meta})
}

// parseDemoListOptions reads the list query under the demo's limits. On an
// invalid value it writes the error and reports false.
func parseDemoListOptions(c *gin.Context) (watchlist.Options, bool) {
	q := c.Request.URL.Query()
	opts := watchlist.Options{
		Status: strings.ToLower(q.Get("status")),
		Type:   strings.ToLower(q.Get("type")),
		Search: strings.TrimSpace(q.Get("search")),
		Sort:   q.Get("sort"),
		Limit:  demoDefaultLimit,
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			jsonError(c, http.StatusBadRequest, "invalid limit")
			return opts, false
		}
		if n == 0 || n > demoMaxLimit {
			n = demoMaxLimit
		}
		opts.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			jsonError(c, http.StatusBadRequest, "invalid offset")
			return opts, false
		}
		opts.Offset = n
	}
	if utf8.RuneCountInString(opts.Search) > demoMaxSearchLen {
		jsonError(c, http.StatusBadRequest, "search is too long")
		return opts, false
	}
	if !validWatchlistStatus(opts.Status) {
		jsonError(c, http.StatusBadRequest, "invalid status filter")
		return opts, false
	}
	if !validWatchlistType(opts.Type) {
		jsonError(c, http.StatusBadRequest, "invalid type filter")
		return opts, false
	}
	var ok bool
	if opts.Services, ok = parseServiceFilter(q["service"]); !ok {
		jsonError(c, http.StatusBadRequest, "invalid service filter")
		return opts, false
	}
	if opts.Available, ok = parseOptionalBool(q.Get("available")); !ok {
		jsonError(c, http.StatusBadRequest, "invalid available filter")
		return opts, false
	}
	return opts, true
}

// DemoService is one entry of GET /api/demo/services.
type DemoService struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Access string `json:"access,omitempty"`
	// Active says whether the demo account has the service.
	Active bool `json:"active"`
}

// DemoServicesResponse is the body of GET /api/demo/services.
type DemoServicesResponse struct {
	Services []DemoService `json:"services"`
}

// demoServicesHandler godoc
// @Summary Public demo services
// @Description The supported streaming services, each marked with whether the demo account has it: the same shape as GET /api/me/services. Public and read-only.
// @Tags Demo
// @Produce json
// @Success 200 {object} DemoServicesResponse "Service catalog with the demo account's selection"
// @Header 200 {string} Cache-Control "public, max-age=300"
// @Failure 404 {object} ErrorResponse "The demo account has not been seeded"
// @Failure 500 {object} ErrorResponse "Failed to load the demo"
// @Router /api/demo/services [get]
func (s *Server) demoServicesHandler(c *gin.Context) {
	snap, ok := s.demoAccount(c)
	if !ok {
		return
	}
	active := snap.user.ActiveServiceCodes()
	out := make([]DemoService, 0, len(models.ServiceCatalog))
	for _, def := range models.ServiceCatalog {
		out = append(out, DemoService{Code: def.Code, Name: def.Name, Access: def.Access, Active: active[def.Code]})
	}
	c.Header("Cache-Control", demoCacheControl)
	c.JSON(http.StatusOK, DemoServicesResponse{Services: out})
}
