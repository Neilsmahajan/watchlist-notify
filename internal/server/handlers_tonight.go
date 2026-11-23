package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/neilsmahajan/watchlist-notify/internal/providers/claude"
	"github.com/neilsmahajan/watchlist-notify/internal/tonight"
)

const (
	// tonightDeadline is how long the whole request may take: the model call
	// (tonight.ModelTimeout, 20 s) with the database reads around it, inside
	// the server's 30 s write timeout.
	tonightDeadline = 27 * time.Second
	// tonightMaxBody is far more than the largest valid request body.
	tonightMaxBody = 8 << 10
)

// tonightRequest is the body of POST /api/recommendations/tonight. Every
// field is optional.
type tonightRequest struct {
	// What the viewer is in the mood for, in their own words. At most 200 characters.
	Mood string `json:"mood" example:"something light"`
	// Keep titles that run no longer than this many minutes (1 to 600). For a show, one episode.
	MaxMinutes *int `json:"max_minutes" example:"120"`
	// Keep only films or only shows.
	Type string `json:"type" enums:"movie,show"`
	// IDs of watchlist items to leave out, such as the ones already suggested. At most 30.
	Exclude []string `json:"exclude"`
}

// TonightLimitResponse is the body of a 429 from the recommendations endpoint.
type TonightLimitResponse struct {
	Error string `json:"error" example:"you have used all 10 of today's picks; the limit resets at 2026-11-23T00:00:00Z"`
	// Which limit: the user's own, or the one shared by all users.
	Scope string `json:"scope" enums:"user,total" example:"user"`
	// When the next day's budget starts.
	ResetsAt time.Time `json:"resets_at" example:"2026-11-23T00:00:00Z"`
}

// tonightHandler godoc
// @Summary Pick what to watch tonight
// @Description Suggests up to three titles from the user's watchlist that are streaming now on their services, with a one-sentence reason each. Anthropic's Claude makes the choice when there are more than three candidates; each such call counts against a per-user and a shared daily limit. With three candidates or fewer they are returned as they are, and with none the response has empty picks and a message. The endpoint answers 503 unless the server has an Anthropic API key.
// @Tags Recommendations
// @Security BearerAuth
// @Accept json
// @Produce json
// @Param body body tonightRequest false "What to pick for; every field is optional"
// @Success 200 {object} tonight.Result "Up to three picks, or none with a message"
// @Failure 400 {object} ErrorResponse "Invalid request body"
// @Failure 401 {object} ErrorResponse "Unauthorized"
// @Failure 403 {object} ErrorResponse "The account is reserved"
// @Failure 429 {object} TonightLimitResponse "A daily limit is reached; no model call was made"
// @Failure 500 {object} ErrorResponse "Database error"
// @Failure 502 {object} ErrorResponse "The model failed or its answer could not be used"
// @Failure 503 {object} ErrorResponse "Recommendations are not enabled on this server"
// @Failure 504 {object} ErrorResponse "The model did not answer in time"
// @Router /api/recommendations/tonight [post]
func (s *Server) tonightHandler(c *gin.Context) {
	if s.recommender == nil {
		jsonError(c, http.StatusServiceUnavailable, "recommendations are not enabled on this server")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), tonightDeadline)
	defer cancel()

	// The demo account is refused here, like on every authenticated route.
	user, ok := s.getUserWithin(ctx, c)
	if !ok {
		return
	}

	var body tonightRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, tonightMaxBody)
	// No body at all asks for anything.
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		jsonError(c, http.StatusBadRequest, "invalid body")
		return
	}
	req, err := tonight.NewRequest(body.Mood, body.MaxMinutes, body.Type, body.Exclude)
	if err != nil {
		jsonError(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := s.recommender.Tonight(ctx, user, req)
	var (
		limitErr *tonight.LimitError
		modelErr *tonight.ModelError
	)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, result)

	case errors.As(err, &limitErr):
		wait := time.Until(limitErr.ResetsAt)
		c.Header("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, TonightLimitResponse{
			Error:    s.limitMessage(limitErr),
			Scope:    limitErr.Scope,
			ResetsAt: limitErr.ResetsAt,
		})

	// What is logged below is the error alone. These errors are built never
	// to hold the mood, the prompt or the model's answer.
	case errors.Is(err, context.DeadlineExceeded):
		log.Printf("tonight: timed out: %v", err)
		jsonError(c, http.StatusGatewayTimeout, "picking took too long; try again")
	case errors.As(err, &modelErr):
		log.Printf("tonight: %v", err)
		jsonError(c, http.StatusBadGateway, "the picks could not be made just now; try again")
	default:
		log.Printf("tonight: %v", err)
		jsonError(c, http.StatusInternalServerError, "failed to pick titles")
	}
}

// limitMessage says which limit was reached and when it resets.
func (s *Server) limitMessage(e *tonight.LimitError) string {
	resets := e.ResetsAt.UTC().Format(time.RFC3339)
	if e.Scope == tonight.ScopeTotal {
		return "today's picks have all been used; the limit resets at " + resets
	}
	return fmt.Sprintf("you have used all %d of today's picks; the limit resets at %s",
		s.recommender.Limits().PerUser, resets)
}

// tonightSettings is the feature's configuration.
type tonightSettings struct {
	apiKey string
	model  string // empty: the client's default
	limits tonight.Limits
}

// loadTonightSettings reads the feature's configuration from the
// environment. When the feature is off, ok is false and reason says why.
//
// The feature is off without ANTHROPIC_API_KEY. It is also off when a limit
// is zero, which makes either variable a switch, or when one is not a
// whole number: a limit that cannot be read must not become "no limit" or
// quietly fall back to a default someone meant to lower.
func loadTonightSettings(getenv func(string) string) (settings tonightSettings, ok bool, reason string) {
	settings.apiKey = strings.TrimSpace(getenv("ANTHROPIC_API_KEY"))
	if settings.apiKey == "" {
		return tonightSettings{}, false, "ANTHROPIC_API_KEY is not set"
	}
	settings.model = strings.TrimSpace(getenv("ANTHROPIC_MODEL"))

	for _, limit := range []struct {
		name     string
		fallback int
		into     *int
	}{
		{"AI_DAILY_LIMIT_PER_USER", tonight.DefaultPerUser, &settings.limits.PerUser},
		{"AI_DAILY_LIMIT_TOTAL", tonight.DefaultTotal, &settings.limits.Total},
	} {
		raw := strings.TrimSpace(getenv(limit.name))
		if raw == "" {
			*limit.into = limit.fallback
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return tonightSettings{}, false, limit.name + " is not a whole number of calls"
		}
		if n == 0 {
			return tonightSettings{}, false, limit.name + " is 0"
		}
		*limit.into = n
	}
	return settings, true, ""
}

// newRecommender builds the recommender from the environment, or returns
// nil when the feature is off. It logs which, and never the key.
func newRecommender(lister tonight.Lister, store tonight.UsageStore, getenv func(string) string) *tonight.Recommender {
	settings, ok, reason := loadTonightSettings(getenv)
	if !ok {
		log.Printf("recommendations: off (%s)", reason)
		return nil
	}
	client, err := claude.New(settings.apiKey, settings.model)
	if err != nil {
		log.Printf("recommendations: off (%v)", err)
		return nil
	}
	log.Printf("recommendations: on, model %s, at most %d calls a day per user and %d in total",
		client.Model(), settings.limits.PerUser, settings.limits.Total)
	return tonight.New(lister, client, store, settings.limits)
}
