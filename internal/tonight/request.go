package tonight

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Bounds on a request.
const (
	MaxMoodLength = 200 // characters
	MaxExclude    = 30  // item IDs
	MaxMinutes    = 600 // the longest "I have this long" worth asking about
)

// Request is what the viewer asked for. The zero value asks for anything.
type Request struct {
	// Mood is free text from the viewer. It is untrusted: it goes to the
	// model as data and is never logged.
	Mood string
	// MaxMinutes keeps titles that run no longer than this; zero does not
	// filter. For a show the runtime is one episode's.
	MaxMinutes int
	// Type is "movie", "show" or empty for both.
	Type string
	// Exclude holds the items already suggested, for "pick again".
	Exclude map[primitive.ObjectID]bool
}

// InvalidRequestError describes a request the caller got wrong. Its text is
// safe to show: it never repeats what was sent.
type InvalidRequestError struct{ Reason string }

func (e *InvalidRequestError) Error() string { return e.Reason }

func invalid(format string, args ...any) error {
	return &InvalidRequestError{Reason: fmt.Sprintf(format, args...)}
}

// NewRequest validates the fields of a request body. A nil maxMinutes means
// the field was absent.
func NewRequest(mood string, maxMinutes *int, mediaType string, exclude []string) (Request, error) {
	req := Request{Mood: cleanText(mood), Type: mediaType}

	if utf8.RuneCountInString(req.Mood) > MaxMoodLength {
		return Request{}, invalid("mood is too long: at most %d characters", MaxMoodLength)
	}
	switch mediaType {
	case "", models.WatchlistTypeMovie, models.WatchlistTypeShow:
	default:
		return Request{}, invalid(`type must be "movie" or "show"`)
	}
	if maxMinutes != nil {
		if *maxMinutes < 1 || *maxMinutes > MaxMinutes {
			return Request{}, invalid("max_minutes must be between 1 and %d", MaxMinutes)
		}
		req.MaxMinutes = *maxMinutes
	}
	if len(exclude) > MaxExclude {
		return Request{}, invalid("exclude holds at most %d item ids", MaxExclude)
	}
	if len(exclude) > 0 {
		req.Exclude = make(map[primitive.ObjectID]bool, len(exclude))
		for _, raw := range exclude {
			id, err := primitive.ObjectIDFromHex(raw)
			if err != nil {
				return Request{}, invalid("exclude must hold watchlist item ids")
			}
			req.Exclude[id] = true
		}
	}
	return req, nil
}

// cleanText makes a piece of text one tidy line: control and formatting
// characters (newlines, tabs, zero-width and direction marks) become
// spaces, and runs of whitespace collapse to one space.
func cleanText(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}

// clip cuts cleaned text to at most limit characters, ending in an ellipsis
// when something was cut.
func clip(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}
