package server

import (
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

// resolveRegion applies fallback: query > user.Region > TMDB_REGION > DEFAULT_REGION > "US"
func resolveRegion(c *gin.Context, user *models.User) string {
	r := strings.TrimSpace(c.Query("region"))
	if r == "" && user != nil && strings.TrimSpace(user.Region) != "" {
		r = user.Region
	}
	if r == "" {
		r = os.Getenv("TMDB_REGION")
	}
	if r == "" {
		r = os.Getenv("DEFAULT_REGION")
	}
	if r == "" {
		r = "US"
	}
	r = strings.ToUpper(r)
	if len(r) != 2 {
		r = "US"
	}
	return r
}

func (s *Server) availabilityHandler(c *gin.Context) {
	if s.tmdb == nil {
		jsonError(c, http.StatusServiceUnavailable, "availability unavailable")
		return
	}
	user, ok := s.getUser(c)
	if !ok {
		return
	}
	typ := strings.ToLower(strings.TrimSpace(c.Query("type")))
	if typ != "movie" && typ != "tv" {
		jsonError(c, http.StatusBadRequest, "invalid type; must be movie or tv")
		return
	}
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		jsonError(c, http.StatusBadRequest, "invalid id")
		return
	}
	region := resolveRegion(c, user)

	// Build quick lookup of user's active services
	active := map[string]bool{}
	for _, svc := range user.Services {
		if svc.Active {
			active[strings.ToLower(svc.Code)] = true
		}
	}

	// Call TMDb providers
	// Use the typed response and then select region
	var providerLink string
	var entries []struct{ ProviderName, LogoPath string }
	if typ == "movie" {
		resp, e := s.tmdb.GetMovieProviders(c.Request.Context(), id)
		if e != nil {
			jsonError(c, http.StatusBadGateway, "upstream providers failed")
			return
		}
		rp, ok := resp.Results[region]
		if !ok {
			// No providers for region
			c.JSON(http.StatusOK, gin.H{"region": region, "providers": []any{}, "unmatched_user_services": codesFromMap(active)})
			return
		}
		providerLink = rp.Link
		// Prefer flatrate, but include free and ads as well for visibility
		for _, p := range rp.Flatrate {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
		for _, p := range rp.Free {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
		for _, p := range rp.Ads {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
	} else {
		resp, e := s.tmdb.GetTVProviders(c.Request.Context(), id)
		if e != nil {
			jsonError(c, http.StatusBadGateway, "upstream providers failed")
			return
		}
		rp, ok := resp.Results[region]
		if !ok {
			c.JSON(http.StatusOK, gin.H{"region": region, "providers": []any{}, "unmatched_user_services": codesFromMap(active)})
			return
		}
		providerLink = rp.Link
		for _, p := range rp.Flatrate {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
		for _, p := range rp.Free {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
		for _, p := range rp.Ads {
			entries = append(entries, struct{ ProviderName, LogoPath string }{p.ProviderName, p.LogoPath})
		}
	}

	// Map to our service codes and intersect with user-active
	type outProv struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		LogoPath string `json:"logo_path,omitempty"`
		Link     string `json:"link,omitempty"`
	}

	var out []outProv
	matchedCodes := map[string]bool{}
	for _, e := range entries {
		if code, ok := models.MapProviderNameToCode(e.ProviderName); ok && active[code] {
			if !matchedCodes[code] { // de-duplicate
				matchedCodes[code] = true
				name := displayNameForCode(code)
				out = append(out, outProv{Code: code, Name: name, LogoPath: e.LogoPath, Link: providerLink})
			}
		}
	}
	// Sort by display name for stability
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	// Compute unmatched user services (still active but not present for title)
	var unmatched []string
	for code, isActive := range active {
		if isActive && !matchedCodes[code] {
			unmatched = append(unmatched, code)
		}
	}
	sort.Strings(unmatched)

	payload := gin.H{
		"region":                  region,
		"providers":               out,
		"unmatched_user_services": unmatched,
	}

	c.JSON(http.StatusOK, payload)
}

func codesFromMap(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func displayNameForCode(code string) string {
	for _, s := range models.ServiceCatalog {
		if s.Code == code {
			return s.Name
		}
	}
	return code
}
