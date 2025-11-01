package server

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

const (
	defaultLimit = 20
	maxLimit     = 1000 // Increased to support larger watchlists
)

// ErrorResponse represents a standard API error response
// @Description Standard error response structure used across all endpoints
type ErrorResponse struct {
	Error string `json:"error" example:"invalid request"`
}

// jsonError standardizes error responses and aborts the context.
func jsonError(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

// getUser fetches the authenticated user from context and DB. It writes the
// appropriate error and returns (nil, false) on failure.
func (s *Server) getUser(c *gin.Context) (*models.User, bool) {
	emailVal, ok := c.Get("user_email")
	if !ok {
		jsonError(c, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	email, _ := emailVal.(string)
	user, err := s.db.GetUserByEmail(c.Request.Context(), email)
	if err != nil {
		jsonError(c, http.StatusInternalServerError, "failed to fetch user")
		return nil, false
	}
	if user == nil {
		// Auto-provision a minimal user record on first authenticated access
		name, _ := c.Get("name")
		picture, _ := c.Get("picture")
		nu := &models.User{
			Email:   email,
			Name:    toString(name),
			Picture: toString(picture),
		}
		if err := s.db.UpsertUser(c.Request.Context(), nu); err != nil {
			jsonError(c, http.StatusInternalServerError, "failed to create user")
			return nil, false
		}
		// Re-fetch to return full record including ID, defaults, etc.
		user, err = s.db.GetUserByEmail(c.Request.Context(), email)
		if err != nil || user == nil {
			jsonError(c, http.StatusInternalServerError, "failed to load user")
			return nil, false
		}
	}
	return user, true
}

// toString safely converts interface{} to string.
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// parseLimitOffset parses limit/offset with defaults and bounds. On invalid
// values it writes an error and returns (0,0,false).
// If limit is set to 0, it returns maxLimit to fetch all items.
func parseLimitOffset(c *gin.Context) (int, int, bool) {
	q := c.Request.URL.Query()
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			jsonError(c, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		if n == 0 {
			// Special case: limit=0 means fetch all items
			limit = maxLimit
		} else if n > maxLimit {
			limit = maxLimit
		} else {
			limit = n
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			jsonError(c, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = n
	}
	return limit, offset, true
}

func validWatchlistStatus(s string) bool {
	switch s {
	case "", models.WatchlistStatusPlanned, models.WatchlistStatusWatching, models.WatchlistStatusFinished:
		return true
	default:
		return false
	}
}

func validWatchlistType(t string) bool {
	switch t {
	case "", models.WatchlistTypeMovie, models.WatchlistTypeShow:
		return true
	default:
		return false
	}
}

// CalculateNextDigestTime calculates when the next digest should be sent
// based on the interval, unit, and last sent time. Exported for use by digest worker.
func CalculateNextDigestTime(interval int, unit string, lastSent *time.Time) time.Time {
	base := time.Now()
	if lastSent != nil && !lastSent.IsZero() {
		base = *lastSent
	}

	switch unit {
	case "days":
		return base.AddDate(0, 0, interval)
	case "weeks":
		return base.AddDate(0, 0, interval*7)
	case "months":
		return base.AddDate(0, interval, 0)
	default:
		// Fallback to 7 days if unit is unrecognized
		return base.AddDate(0, 0, 7)
	}
}
