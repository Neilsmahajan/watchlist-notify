package models

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TMDb media types. Watchlist items say "show" where TMDb says "tv".
const (
	MediaTypeMovie = "movie"
	MediaTypeTV    = "tv"
)

// Offer access types. The first three are "streaming": a subscriber (or
// anyone, for free/ads) can press play. Rent and buy are stored for display
// but never count as availability and never produce events.
const (
	AccessSubscription = ServiceAccessSubscription
	AccessFree         = ServiceAccessFree
	AccessAds          = ServiceAccessAds
	AccessRent         = "rent"
	AccessBuy          = "buy"
)

// StreamingAccessOrder lists the streaming access types, best first.
var StreamingAccessOrder = []string{AccessSubscription, AccessFree, AccessAds}

// IsStreamingAccess reports whether an access type means "included, press play".
func IsStreamingAccess(access string) bool {
	switch access {
	case AccessSubscription, AccessFree, AccessAds:
		return true
	default:
		return false
	}
}

// TMDbMediaType converts a watchlist item type or a TMDb media type to the
// TMDb media type.
func TMDbMediaType(itemType string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case WatchlistTypeMovie:
		return MediaTypeMovie, true
	case WatchlistTypeShow, MediaTypeTV:
		return MediaTypeTV, true
	default:
		return "", false
	}
}

// TitleRef identifies a title on TMDb.
type TitleRef struct {
	MediaType string // movie | tv
	TMDbID    int
}

// Key is the deterministic _id of the title's document: "movie:603", "tv:1399".
func (r TitleRef) Key() string {
	return r.MediaType + ":" + strconv.Itoa(r.TMDbID)
}

// ParseTitleKey is the inverse of TitleRef.Key.
func ParseTitleKey(key string) (TitleRef, error) {
	mediaType, idStr, found := strings.Cut(key, ":")
	if !found || (mediaType != MediaTypeMovie && mediaType != MediaTypeTV) {
		return TitleRef{}, fmt.Errorf("invalid title key %q", key)
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return TitleRef{}, fmt.Errorf("invalid title key %q", key)
	}
	return TitleRef{MediaType: mediaType, TMDbID: id}, nil
}

// NormalizeRegion upper-cases an ISO 3166-1 alpha-2 code. ok is false for
// anything that is not exactly two ASCII letters, which also makes the result
// safe to use inside a MongoDB field path.
func NormalizeRegion(region string) (string, bool) {
	region = strings.ToUpper(strings.TrimSpace(region))
	if len(region) != 2 {
		return "", false
	}
	for i := 0; i < len(region); i++ {
		if region[i] < 'A' || region[i] > 'Z' {
			return "", false
		}
	}
	return region, true
}

// Title is one document in the shared `titles` collection: TMDb metadata plus
// the latest watch-provider snapshot for every tracked region.
type Title struct {
	ID        string `bson:"_id" json:"id"`
	MediaType string `bson:"media_type" json:"media_type"`
	TMDbID    int    `bson:"tmdb_id" json:"tmdb_id"`

	TitleMetadata `bson:",inline"`

	// Providers holds one snapshot per tracked region, keyed by region code.
	Providers map[string]RegionSnapshot `bson:"providers,omitempty" json:"providers,omitempty"`

	// NotFoundAt is set when TMDb answered 404 for this title, so the read
	// path stops asking on every request.
	NotFoundAt *time.Time `bson:"not_found_at,omitempty" json:"not_found_at,omitempty"`

	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

// Ref returns the TMDb reference for this title.
func (t *Title) Ref() TitleRef {
	return TitleRef{MediaType: t.MediaType, TMDbID: t.TMDbID}
}

// Snapshot returns the stored provider snapshot for a region.
func (t *Title) Snapshot(region string) (RegionSnapshot, bool) {
	if t == nil {
		return RegionSnapshot{}, false
	}
	snap, ok := t.Providers[region]
	return snap, ok
}

// TitleMetadata is the descriptive part of a title, refreshed weekly.
type TitleMetadata struct {
	Title             string     `bson:"title,omitempty" json:"title,omitempty"`
	Year              int        `bson:"year,omitempty" json:"year,omitempty"`
	PosterPath        string     `bson:"poster_path,omitempty" json:"poster_path,omitempty"`
	IMDbID            string     `bson:"imdb_id,omitempty" json:"imdb_id,omitempty"`
	Runtime           int        `bson:"runtime,omitempty" json:"runtime,omitempty"` // minutes
	Genres            []string   `bson:"genres,omitempty" json:"genres,omitempty"`
	Rating            float64    `bson:"rating,omitempty" json:"rating,omitempty"` // TMDb vote_average, 0-10
	VoteCount         int        `bson:"vote_count,omitempty" json:"vote_count,omitempty"`
	Overview          string     `bson:"overview,omitempty" json:"overview,omitempty"`
	MetadataFetchedAt *time.Time `bson:"metadata_fetched_at,omitempty" json:"metadata_fetched_at,omitempty"`
}

// RegionSnapshot is what TMDb reported for one title in one region at
// FetchedAt. FetchedAt doubles as the snapshot's version: it is what the
// compare-and-set in the title store and the event IDs are keyed on.
type RegionSnapshot struct {
	Link      string          `bson:"link,omitempty" json:"link,omitempty"`
	Offers    []ProviderOffer `bson:"offers" json:"offers"`
	FetchedAt time.Time       `bson:"providers_fetched_at" json:"providers_fetched_at"`
}

// ProviderOffer is one provider offering a title under one access type.
type ProviderOffer struct {
	ProviderID   int    `bson:"provider_id" json:"provider_id"`
	ProviderName string `bson:"provider_name" json:"provider_name"`
	LogoPath     string `bson:"logo_path,omitempty" json:"logo_path,omitempty"`
	// Code is the service code the provider mapped to when the snapshot was
	// taken. It is stored for inspection and ad-hoc queries only; readers call
	// ServiceCode so that a later fix to the mapping table applies to stored
	// snapshots too, and so that such a fix can never look like a title
	// "arriving" on a service.
	Code   string `bson:"code,omitempty" json:"code,omitempty"`
	Access string `bson:"access" json:"access"` // subscription | free | ads | rent | buy
}

// ServiceCode resolves the offer's provider against the current mapping table.
func (o ProviderOffer) ServiceCode() (string, bool) {
	return MapProvider(o.ProviderID, o.ProviderName)
}

// ServiceAvailability is one of our services streaming a title.
type ServiceAvailability struct {
	Code         string
	ProviderName string   // TMDb's name for the provider behind the best offer
	LogoPath     string   // TMDb logo path of that provider
	Access       []string // streaming access types on offer, best first
}

// BestAccess returns the most favorable access type on offer.
func (s ServiceAvailability) BestAccess() string {
	if len(s.Access) == 0 {
		return AccessSubscription
	}
	return s.Access[0]
}

// Services returns the services (by our codes) that stream the title in this
// snapshot. Rent and buy offers, and providers that map to no service, are
// left out. The order is deterministic: best access first, then TMDb's order.
func (s RegionSnapshot) Services() []ServiceAvailability {
	var out []ServiceAvailability
	index := make(map[string]int)
	for _, access := range StreamingAccessOrder {
		for _, offer := range s.Offers {
			if offer.Access != access {
				continue
			}
			code, ok := offer.ServiceCode()
			if !ok {
				continue
			}
			i, seen := index[code]
			if !seen {
				index[code] = len(out)
				out = append(out, ServiceAvailability{
					Code:         code,
					ProviderName: offer.ProviderName,
					LogoPath:     offer.LogoPath,
					Access:       []string{access},
				})
				continue
			}
			if last := out[i].Access[len(out[i].Access)-1]; last != access {
				out[i].Access = append(out[i].Access, access)
			}
		}
	}
	return out
}

// HasService reports whether the service streams the title in this snapshot.
func (s RegionSnapshot) HasService(code string) bool {
	for _, svc := range s.Services() {
		if svc.Code == code {
			return true
		}
	}
	return false
}

// Availability change kinds.
const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
)

// AvailabilityEvent records one service starting or stopping to stream a
// title in a region. Events are append-only and expire through a TTL index.
type AvailabilityEvent struct {
	// ID is derived from (title, region, service, change, version of the
	// snapshot the change was diffed against), so re-running a diff against
	// the same stored snapshot cannot create a second event.
	ID           string    `bson:"_id" json:"id"`
	TitleKey     string    `bson:"title_key" json:"title_key"`
	Region       string    `bson:"region" json:"region"`
	ServiceCode  string    `bson:"service_code" json:"service_code"`
	ProviderName string    `bson:"provider_name,omitempty" json:"provider_name,omitempty"`
	Access       string    `bson:"access" json:"access"`
	Change       string    `bson:"change" json:"change"` // added | removed
	DetectedAt   time.Time `bson:"detected_at" json:"detected_at"`
	ExpiresAt    time.Time `bson:"expires_at" json:"expires_at"`
}

// EventRetention is how long availability events are kept.
const EventRetention = 90 * 24 * time.Hour
