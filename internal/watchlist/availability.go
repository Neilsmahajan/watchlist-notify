// Package watchlist builds the view of a user's watchlist that the API
// serves: every item joined with its stored title metadata and streaming
// availability, then filtered, sorted and paged. It has no HTTP in it.
package watchlist

import (
	"sort"
	"strconv"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// Availability is where a title can be watched in one region, from one
// user's point of view.
type Availability struct {
	Region    string    `json:"region"`
	CheckedAt time.Time `json:"checked_at"`
	// Link is TMDb's "where to watch" page for the title and region.
	Link string `json:"link,omitempty"`
	// Providers are the user's active services that stream the title.
	Providers []Provider `json:"providers"`
	// Elsewhere is everything else: services the user does not have, and
	// rent or buy offers.
	Elsewhere []Provider `json:"elsewhere"`
}

// Provider is one service or storefront offering a title.
type Provider struct {
	// Code is our service code; empty for providers we do not model.
	Code     string   `json:"code,omitempty"`
	Name     string   `json:"name"`
	LogoPath string   `json:"logo_path,omitempty"`
	Access   []string `json:"access"` // subscription | free | ads | rent | buy
}

// accessOrder ranks access types for display, cheapest for the viewer first.
var accessOrder = []string{
	models.AccessSubscription, models.AccessFree, models.AccessAds, models.AccessRent, models.AccessBuy,
}

// BuildAvailability turns a stored snapshot into a user's view of it. active
// holds the service codes the user has.
//
// Offers are grouped by service code, so "Paramount Plus Premium" and
// "Paramount Plus Essential" show up once, as Paramount+. Providers without a
// code are grouped by TMDb provider ID. A service counts as one of the user's
// only if it streams the title; a service the user has that merely rents it
// out is listed under Elsewhere.
func BuildAvailability(snap models.RegionSnapshot, region string, active map[string]bool) *Availability {
	type group struct {
		provider Provider
		access   map[string]bool
	}
	var order []string
	groups := make(map[string]*group)
	for _, offer := range snap.Offers {
		code, mapped := offer.ServiceCode()
		key, name := "id:"+strconv.Itoa(offer.ProviderID), offer.ProviderName
		if mapped {
			key, name = code, models.ServiceName(code)
		} else {
			code = ""
		}
		g, ok := groups[key]
		if !ok {
			g = &group{
				provider: Provider{Code: code, Name: name, LogoPath: offer.LogoPath},
				access:   make(map[string]bool),
			}
			groups[key] = g
			order = append(order, key)
		}
		if g.provider.LogoPath == "" {
			g.provider.LogoPath = offer.LogoPath
		}
		g.access[offer.Access] = true
	}

	out := &Availability{
		Region:    region,
		CheckedAt: snap.FetchedAt,
		Link:      snap.Link,
		Providers: []Provider{},
		Elsewhere: []Provider{},
	}
	for _, key := range order {
		g := groups[key]
		streams := false
		for _, access := range models.StreamingAccessOrder {
			streams = streams || g.access[access]
		}
		mine := g.provider.Code != "" && active[g.provider.Code] && streams
		for _, access := range accessOrder {
			if !g.access[access] {
				continue
			}
			if mine && !models.IsStreamingAccess(access) {
				continue
			}
			g.provider.Access = append(g.provider.Access, access)
		}
		if mine {
			out.Providers = append(out.Providers, g.provider)
		} else {
			out.Elsewhere = append(out.Elsewhere, g.provider)
		}
	}

	sort.SliceStable(out.Providers, func(i, j int) bool { return out.Providers[i].Name < out.Providers[j].Name })
	sort.SliceStable(out.Elsewhere, func(i, j int) bool {
		a, b := out.Elsewhere[i], out.Elsewhere[j]
		if sa, sb := streamable(a), streamable(b); sa != sb {
			return sa // streaming offers before rent and buy
		}
		return a.Name < b.Name
	})
	return out
}

func streamable(p Provider) bool {
	for _, access := range p.Access {
		if models.IsStreamingAccess(access) {
			return true
		}
	}
	return false
}

// Unmatched returns the user's active service codes that do not stream the
// title, sorted.
func (a *Availability) Unmatched(active map[string]bool) []string {
	matched := make(map[string]bool)
	if a != nil {
		for _, p := range a.Providers {
			matched[p.Code] = true
		}
	}
	unmatched := []string{}
	for code, on := range active {
		if on && !matched[code] {
			unmatched = append(unmatched, code)
		}
	}
	sort.Strings(unmatched)
	return unmatched
}
