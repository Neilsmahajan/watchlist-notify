package server

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	return s.getUserWithin(c.Request.Context(), c)
}

// getUserWithin is getUser with the database reads on ctx, for handlers
// that answer by a deadline: the first read of a request counts against it
// like every later one.
func (s *Server) getUserWithin(ctx context.Context, c *gin.Context) (*models.User, bool) {
	emailVal, ok := c.Get("user_email")
	if !ok {
		jsonError(c, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	email, _ := emailVal.(string)
	name, _ := c.Get("name")
	picture, _ := c.Get("picture")

	user, err := s.resolveUser(ctx, email, toString(name), toString(picture))
	switch {
	case errors.Is(err, errReservedAccount):
		jsonError(c, http.StatusForbidden, err.Error())
		return nil, false
	case err != nil:
		// The other failures are the database's; the text says which step.
		log.Printf("user lookup: %v", err)
		jsonError(c, http.StatusInternalServerError, userLookupMessage(err))
		return nil, false
	}
	return user, true
}

// What resolveUser can fail with. Each wraps its cause, where there is one,
// for the log; the text of the sentinel is what a client is shown.
var (
	// errReservedAccount: the email is the demo account's.
	errReservedAccount = errors.New("this account is reserved")
	errFetchUser       = errors.New("failed to fetch user")
	errCreateUser      = errors.New("failed to create user")
	errLoadUser        = errors.New("failed to load user")
)

// userLookupMessage returns the sentinel text of a resolveUser error, without
// the cause it wraps.
func userLookupMessage(err error) string {
	for _, sentinel := range []error{errReservedAccount, errFetchUser, errCreateUser, errLoadUser} {
		if errors.Is(err, sentinel) {
			return sentinel.Error()
		}
	}
	return "failed to fetch user"
}

// resolveUser returns the account of an authenticated caller, identified by
// the email their token carries. It is the one place a token's identity
// becomes a user: the REST handlers and the MCP tools both go through it, so
// both find the same account and both create it the same way on first use.
//
// The caller is responsible for the email being verified by the issuer.
func (s *Server) resolveUser(ctx context.Context, email, name, picture string) (*models.User, error) {
	if isReservedEmail(email) {
		// The demo account is served read-only by the public demo endpoints.
		// It must never be usable as a login, whoever registers its address.
		return nil, errReservedAccount
	}
	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFetchUser, err)
	}
	if user != nil {
		return user, nil
	}

	// Auto-provision a minimal user record on first authenticated access.
	if err := s.db.UpsertUser(ctx, &models.User{Email: email, Name: name, Picture: picture}); err != nil {
		return nil, fmt.Errorf("%w: %w", errCreateUser, err)
	}
	// Re-fetch to return the full record including ID, defaults, etc.
	user, err = s.db.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errLoadUser, err)
	}
	if user == nil {
		return nil, errLoadUser
	}
	return user, nil
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

// upstreamLookupBudget is how long a request may spend on one TMDb lookup,
// retries included. The TMDb client backs off on rate limiting and gateway
// errors for up to about half a minute, which suits the background worker.
// A request must answer well inside the server's 30 s write timeout, so the
// handlers that call TMDb directly put this deadline on the call and report
// a timeout instead of letting the connection be cut.
const upstreamLookupBudget = 8 * time.Second

// lookupContext returns ctx limited to the upstream lookup budget.
func (s *Server) lookupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	budget := s.lookupBudget
	if budget <= 0 {
		budget = upstreamLookupBudget
	}
	return context.WithTimeout(ctx, budget)
}
