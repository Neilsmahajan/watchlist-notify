package server

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/models"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

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
	if err != nil || user == nil {
		jsonError(c, http.StatusUnauthorized, "user not found")
		return nil, false
	}
	return user, true
}

// parseLimitOffset parses limit/offset with defaults and bounds. On invalid
// values it writes an error and returns (0,0,false).
func parseLimitOffset(c *gin.Context) (int, int, bool) {
	q := c.Request.URL.Query()
	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			jsonError(c, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = n
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
