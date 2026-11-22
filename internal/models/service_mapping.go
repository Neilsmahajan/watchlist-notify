package models

import (
	"regexp"
	"strings"
)

// TMDb identifies a streaming provider by a numeric ID and a display name.
// Display names drift ("Max" became "HBO Max", "Apple TV+" became "Apple TV",
// Paramount+ split into "Paramount Plus Premium" and "Paramount Plus
// Essential"), so the ID is the primary key and the normalized name is only a
// fallback for IDs this table has not seen yet.
//
// A provider maps to one of our service codes only when a subscriber to that
// service can actually watch the title: the base provider and its same-catalog
// plan variants. Resale channels ("Paramount+ Amazon Channel", "Starz Apple TV
// Channel", "AMC+ Roku Premium Channel") are separate subscriptions and never
// map to the underlying service.

// notAService marks provider IDs that are known and deliberately unmapped, so
// the name fallback cannot claim them.
const notAService = ""

// providerIDs maps TMDb provider IDs to service codes.
var providerIDs = map[int]string{
	8:    ServiceNetflix,       // Netflix
	175:  ServiceNetflix,       // Netflix Kids
	1796: ServiceNetflix,       // Netflix Standard with Ads
	9:    ServicePrimeVideo,    // Amazon Prime Video
	2100: ServicePrimeVideo,    // Amazon Prime Video with Ads
	15:   ServiceHulu,          // Hulu
	337:  ServiceDisneyPlus,    // Disney Plus
	1899: ServiceMax,           // HBO Max (was "Max")
	384:  ServiceMax,           // HBO Max (legacy ID)
	531:  ServiceParamountPlus, // Paramount Plus
	1770: ServiceParamountPlus, // Paramount+ with Showtime
	2303: ServiceParamountPlus, // Paramount Plus Premium
	2616: ServiceParamountPlus, // Paramount Plus Essential
	386:  ServicePeacock,       // Peacock Premium
	387:  ServicePeacock,       // Peacock Premium Plus
	350:  ServiceAppleTVPlus,   // Apple TV (was "Apple TV Plus")
	300:  ServicePlutoTV,       // Pluto TV
	73:   ServiceTubi,          // Tubi TV
	207:  ServiceRokuChannel,   // The Roku Channel
	538:  ServicePlex,          // Plex
	34:   ServiceMGMPlus,       // MGM Plus
	283:  ServiceCrunchyroll,   // Crunchyroll
	43:   ServiceStarz,         // Starz
	526:  ServiceAMCPlus,       // AMC+
	1768: ServiceESPNPlus,      // ESPN Plus
	520:  ServiceDiscoveryPlus, // Discovery+

	// Storefronts that share a name with a subscription service. They only
	// ever appear as rent/buy offers, and must not match by name.
	2:  notAService, // Apple TV Store (rent/buy)
	10: notAService, // Amazon Video (rent/buy)
}

// providerNames maps normalized TMDb provider names to service codes. It is
// consulted only for provider IDs that providerIDs does not know.
var providerNames = map[string]string{
	"netflix":                   ServiceNetflix,
	"netflixkids":               ServiceNetflix,
	"netflixbasicwithads":       ServiceNetflix,
	"netflixstandardwithads":    ServiceNetflix,
	"primevideo":                ServicePrimeVideo,
	"amazonprimevideo":          ServicePrimeVideo,
	"amazonprimevideowithads":   ServicePrimeVideo,
	"hulu":                      ServiceHulu,
	"disneyplus":                ServiceDisneyPlus,
	"max":                       ServiceMax,
	"hbomax":                    ServiceMax,
	"paramountplus":             ServiceParamountPlus,
	"paramountpluspremium":      ServiceParamountPlus,
	"paramountplusessential":    ServiceParamountPlus,
	"paramountpluswithshowtime": ServiceParamountPlus,
	"peacock":                   ServicePeacock,
	"peacockpremium":            ServicePeacock,
	"peacockpremiumplus":        ServicePeacock,
	"appletvplus":               ServiceAppleTVPlus,
	"appletv":                   ServiceAppleTVPlus,
	"plutotv":                   ServicePlutoTV,
	"tubi":                      ServiceTubi,
	"tubitv":                    ServiceTubi,
	"therokuchannel":            ServiceRokuChannel,
	"rokuchannel":               ServiceRokuChannel,
	"plex":                      ServicePlex,
	"mgmplus":                   ServiceMGMPlus,
	"crunchyroll":               ServiceCrunchyroll,
	"starz":                     ServiceStarz,
	"amcplus":                   ServiceAMCPlus,
	"espnplus":                  ServiceESPNPlus,
	"discoveryplus":             ServiceDiscoveryPlus,
}

var (
	nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
	// resaleChannel matches third-party storefront bundles of another service.
	resaleChannel = regexp.MustCompile(`(amazon|appletv|rokupremium)channels?$`)
)

func normalizeProviderName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Spell out symbols before stripping punctuation so "Disney+" and
	// "Disney Plus" normalize to the same key.
	s = strings.NewReplacer("+", "plus", "&", "and").Replace(s)
	return nonAlphaNum.ReplaceAllString(s, "")
}

// MapProvider resolves a TMDb provider to one of our service codes. The
// provider ID wins when it is known; otherwise the normalized display name is
// tried. Resale channels never map.
func MapProvider(providerID int, providerName string) (string, bool) {
	if code, known := providerIDs[providerID]; known {
		return code, code != notAService
	}
	key := normalizeProviderName(providerName)
	if key == "" || resaleChannel.MatchString(key) {
		return "", false
	}
	code, ok := providerNames[key]
	return code, ok
}

// MappedByID reports whether the provider ID is in the ID table. The refresh
// job uses it to report providers that only matched through the name fallback.
func MappedByID(providerID int) bool {
	code, known := providerIDs[providerID]
	return known && code != notAService
}
