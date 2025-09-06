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
	"disney+":    ServiceDisneyPlus,
	"disneyplus": ServiceDisneyPlus,
	"disney":     ServiceDisneyPlus, // occasionally used
	// Max (formerly HBO Max)
	"max":    ServiceMax,
	"hbomax": ServiceMax,
}

func normalizeProviderName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Keep a fast path for common names with '+' in them
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
