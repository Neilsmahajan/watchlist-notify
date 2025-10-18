package models

import (
	"regexp"
	"strings"
)

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// providerAliases maps normalized TMDb provider names to our stable service codes.
var providerAliases = map[string]string{
	// Netflix
	"netflix": ServiceNetflix,
	// Prime Video
	"primevideo":       ServicePrimeVideo,
	"amazonprimevideo": ServicePrimeVideo,
	"amazonvideo":      ServicePrimeVideo,
	// Hulu
	"hulu": ServiceHulu,
	// Disney+
	"disneyplus": ServiceDisneyPlus,
	"disney":     ServiceDisneyPlus, // occasionally used
	// Max (formerly HBO Max)
	"max":    ServiceMax,
	"hbomax": ServiceMax,
	// Paramount+
	"paramountplus": ServiceParamountPlus,
	"paramount":     ServiceParamountPlus,
	// Peacock
	"peacock":            ServicePeacock,
	"peacockpremium":     ServicePeacock,
	"peacockpremiumplus": ServicePeacock,
	// Apple TV+
	"appletvplus": ServiceAppleTVPlus,
	"appletv":     ServiceAppleTVPlus,
	// Pluto TV
	"plutotv": ServicePlutoTV,
	"pluto":   ServicePlutoTV,
	// Tubi
	"tubi":   ServiceTubi,
	"tubitv": ServiceTubi,
	// The Roku Channel
	"therokuchannel": ServiceRokuChannel,
	"rokuchannel":    ServiceRokuChannel,
	"roku":           ServiceRokuChannel,
	// Plex
	"plex": ServicePlex,
	// MGM+
	"mgmplus": ServiceMGMPlus,
	"mgm":     ServiceMGMPlus,
	// Crunchyroll
	"crunchyroll": ServiceCrunchyroll,
	// Starz
	"starz": ServiceStarz,
	// AMC+
	"amcplus": ServiceAMCPlus,
	"amc":     ServiceAMCPlus,
	// ESPN+
	"espnplus": ServiceESPNPlus,
	"espn":     ServiceESPNPlus,
	// Discovery+
	"discoveryplus": ServiceDiscoveryPlus,
	"discovery":     ServiceDiscoveryPlus,
}

func normalizeProviderName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	// Normalise common punctuation before stripping non-alphanumerics so "HBO+" -> "hboplus".
	replacer := strings.NewReplacer(
		"+", "plus",
		"&", "and",
	)
	s = replacer.Replace(s)
	s = strings.ReplaceAll(s, " ", "")
	// For any remaining punctuation, strip to alphanum
	s = nonAlphaNum.ReplaceAllString(s, "")
	return s
}

// MapProviderNameToCode maps a TMDb provider display name to our service code.
func MapProviderNameToCode(name string) (string, bool) {
	key := normalizeProviderName(name)
	code, ok := providerAliases[key]
	return code, ok
}
