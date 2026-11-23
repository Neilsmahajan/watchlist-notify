// Package tonight answers "what should I watch tonight?": it picks up to
// three titles from a user's watchlist that are streaming on their services
// right now, and has a language model make the choice when there is one to
// make. It has no HTTP in it and does not know which model it is talking to.
//
// A model call costs money and reads text the application does not control,
// and the package is arranged around both facts:
//
//   - The model is called only when there are more candidates than picks,
//     and only after a place has been taken in two daily budgets (quota.go).
//   - The model sees a short, fixed description of each candidate under a
//     small index and answers with indexes. The instructions are a constant;
//     watchlist text and the viewer's mood travel separately, marked as data
//     (prompt.go).
//   - The answer is used for nothing but those indexes and a line of display
//     text each, both checked (answer.go).
//
// Nothing in the package logs a mood, a prompt or an answer.
package tonight

import (
	"context"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/neilsmahajan/watchlist-notify/internal/models"
	"github.com/neilsmahajan/watchlist-notify/internal/watchlist"
)

const (
	// MaxPicks is how many titles one answer suggests.
	MaxPicks = 3
	// MaxCandidates caps how many titles are described to the model, and with
	// it the size and the price of a call. A longer list is sampled.
	MaxCandidates = 120
	// ModelTimeout is the deadline on the model call. The API's write timeout
	// is 30 seconds; this leaves room for the database on either side.
	ModelTimeout = 20 * time.Second
)

// Why an answer has no picks.
const (
	// EmptyNoServices: the user has not said which services they have.
	EmptyNoServices = "no_services"
	// EmptyNothingStreaming: nothing they still want to watch is streaming
	// on those services.
	EmptyNothingStreaming = "nothing_streaming"
	// EmptyNoMatch: titles are streaming, but none fits the type or the
	// running time asked for.
	EmptyNoMatch = "no_match"
	// EmptyExhausted: every title that fits was excluded, because it has
	// been suggested already.
	EmptyExhausted = "exhausted"
)

var emptyMessages = map[string]string{
	EmptyNoServices:       "Choose the streaming services you have, and tonight's picks will come from what they are showing.",
	EmptyNothingStreaming: "Nothing you still want to watch is streaming on your services right now.",
	EmptyNoMatch:          "Nothing streaming on your services fits that. Try a longer running time, or films and shows together.",
	EmptyExhausted:        "That was every title that fits. Start over to see them again.",
}

// Lister builds the user's watchlist view; *watchlist.Lister is the real one.
type Lister interface {
	List(ctx context.Context, user *models.User, opts watchlist.Options) (*watchlist.View, error)
}

// Model is the language model, reduced to what this package needs of it.
type Model interface {
	// Model returns the model's ID, for the response.
	Model() string
	// CompleteJSON returns the model's answer to one system prompt and one
	// user message, as JSON in the shape of schema.
	CompleteJSON(ctx context.Context, system, user string, schema map[string]any) (string, error)
}

// ModelError says the model call failed or its answer could not be used.
// The call was made and counted. Its text is safe to log: it holds no part
// of a prompt or an answer.
type ModelError struct{ Err error }

func (e *ModelError) Error() string { return "tonight: " + e.Err.Error() }
func (e *ModelError) Unwrap() error { return e.Err }

// Pick is one suggestion.
type Pick struct {
	// Item is the watchlist item, as the watchlist endpoint returns it.
	Item watchlist.Item `json:"item"`
	// Reason is one sentence on why it suits tonight.
	Reason string `json:"reason"`
}

// Result is the answer to one request.
type Result struct {
	Picks []Pick `json:"picks"`
	// RemainingToday is how many more model calls the user may make before
	// the next UTC day.
	RemainingToday int `json:"remaining_today"`
	// Model is the ID of the model that chose, or empty when no model was
	// called: there was nothing to choose between.
	Model string `json:"model"`
	// Message and EmptyReason explain an answer without picks.
	Message     string `json:"message,omitempty"`
	EmptyReason string `json:"empty_reason,omitempty" enums:"no_services,nothing_streaming,no_match,exhausted"`
}

// Recommender picks what to watch tonight.
type Recommender struct {
	lister Lister
	model  Model
	quota  quota
	// shuffle orders the candidates at random; tests replace it.
	shuffle func(n int, swap func(i, j int))
	timeout time.Duration
}

// New returns a Recommender that lets model choose, within limits.
func New(lister Lister, model Model, store UsageStore, limits Limits) *Recommender {
	return &Recommender{
		lister:  lister,
		model:   model,
		quota:   quota{store: store, limits: limits, now: time.Now},
		shuffle: rand.Shuffle,
		timeout: ModelTimeout,
	}
}

// Model returns the ID of the model that makes the choices.
func (r *Recommender) Model() string { return r.model.Model() }

// Limits returns the daily limits in force.
func (r *Recommender) Limits() Limits { return r.quota.limits }

// Tonight suggests up to MaxPicks titles for the user.
//
// The model is called only when it has a choice to make: with no candidates
// the result explains why, and with MaxPicks or fewer they are all returned
// as they are. Neither case counts against the daily limits.
//
// It returns a *LimitError when a daily limit is reached, in which case no
// call was made, and a *ModelError when the call was made but produced
// nothing usable. Any other error is the database's.
func (r *Recommender) Tonight(ctx context.Context, user *models.User, req Request) (*Result, error) {
	candidates, emptyReason, err := r.candidates(ctx, user, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) <= MaxPicks {
		remaining, err := r.quota.remaining(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		result := &Result{Picks: []Pick{}, RemainingToday: remaining}
		if len(candidates) == 0 {
			result.EmptyReason, result.Message = emptyReason, emptyMessages[emptyReason]
		}
		for i := range candidates {
			result.Picks = append(result.Picks, Pick{Item: candidates[i], Reason: plainReason(&candidates[i])})
		}
		return result, nil
	}

	// From here on a model call is made, and paid for: take the places first.
	remaining, err := r.quota.reserve(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	shown := r.sample(candidates)
	system, message := buildPrompt(shown, req.Mood)
	callCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	answer, err := r.model.CompleteJSON(callCtx, system, message, answerSchema)
	if err != nil {
		return nil, &ModelError{Err: err}
	}
	choices, err := parseAnswer(answer, len(shown))
	if err != nil {
		return nil, &ModelError{Err: err}
	}

	result := &Result{Picks: make([]Pick, 0, len(choices)), RemainingToday: remaining, Model: r.model.Model()}
	for _, c := range choices {
		item := shown[c.position]
		reason := c.reason
		if reason == "" {
			reason = plainReason(&item)
		}
		result.Picks = append(result.Picks, Pick{Item: item, Reason: reason})
	}
	return result, nil
}

// candidates returns the titles the user could watch tonight that fit the
// request. When there are none it says why.
func (r *Recommender) candidates(ctx context.Context, user *models.User, req Request) ([]watchlist.Item, string, error) {
	if len(user.ActiveServiceCodes()) == 0 {
		return nil, EmptyNoServices, nil
	}
	streamingNow := true
	view, err := r.lister.List(ctx, user, watchlist.Options{Available: &streamingNow})
	if err != nil {
		return nil, "", err
	}

	// Still to watch: a finished title is not a suggestion for tonight.
	var streamable []watchlist.Item
	for _, item := range view.Items {
		if item.Status == models.WatchlistStatusPlanned || item.Status == models.WatchlistStatusWatching {
			streamable = append(streamable, item)
		}
	}
	if len(streamable) == 0 {
		return nil, EmptyNothingStreaming, nil
	}

	var fitting []watchlist.Item
	for _, item := range streamable {
		if req.Type != "" && item.Type != req.Type {
			continue
		}
		// A title with no known runtime cannot be promised to fit.
		if req.MaxMinutes > 0 && (item.Runtime <= 0 || item.Runtime > req.MaxMinutes) {
			continue
		}
		fitting = append(fitting, item)
	}
	if len(fitting) == 0 {
		return nil, EmptyNoMatch, nil
	}

	var fresh []watchlist.Item
	for _, item := range fitting {
		if !req.Exclude[item.ID] {
			fresh = append(fresh, item)
		}
	}
	if len(fresh) == 0 {
		return nil, EmptyExhausted, nil
	}
	return fresh, "", nil
}

// sample returns the candidates to describe to the model, in random order
// and at most MaxCandidates of them.
//
// A list longer than the cap is cut after the shuffle, which makes the cut a
// uniform sample: every title has the same chance of being considered,
// rather than the newest or the first alphabetically always winning. The
// shuffle matters for a short list too. Models favour what they read first,
// and a fixed order would suggest the same few titles every evening.
func (r *Recommender) sample(candidates []watchlist.Item) []watchlist.Item {
	shown := append([]watchlist.Item(nil), candidates...)
	r.shuffle(len(shown), func(i, j int) { shown[i], shown[j] = shown[j], shown[i] })
	if len(shown) > MaxCandidates {
		shown = shown[:MaxCandidates]
	}
	return shown
}

// plainReason is the reason given for a pick the model did not explain:
// where it is streaming, which is true of every candidate.
func plainReason(item *watchlist.Item) string {
	names := serviceNames(item)
	switch len(names) {
	case 0:
		return "On your list and streaming now."
	case 1:
		return "Streaming now on " + names[0] + "."
	default:
		return "Streaming now on " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + "."
	}
}
