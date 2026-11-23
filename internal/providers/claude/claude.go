// Package claude is the application's client for Anthropic's Claude API. It
// does one thing: ask a model a question and get back JSON that follows a
// schema. It knows nothing about watchlists; the feature that uses it builds
// the prompt and checks the answer.
//
// The client never logs what it sends or what the model answers, and the
// errors it returns carry no prompt, no answer and no response body, so they
// are safe to log.
package claude

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

const (
	// DefaultModel is Claude Haiku 5.5: the fastest and cheapest current
	// model, and one that supports structured outputs. Picking three titles
	// from a short list of facts is well within it.
	DefaultModel = "claude-haiku-5-5"

	// maxTokens bounds what one call can cost in output. The answer itself is
	// about 150 tokens of JSON, but Haiku 5.5 thinks before it answers and
	// that thinking counts toward the same limit, so the limit leaves room
	// for it. A limit sized for the answer alone would cut the answer off.
	maxTokens = 1024
)

var (
	// ErrRefused means the model declined to answer (stop reason "refusal").
	ErrRefused = errors.New("claude: the model declined to answer")
	// ErrTruncated means the answer hit the max_tokens limit, so the JSON is
	// incomplete and must not be used.
	ErrTruncated = errors.New("claude: the answer was cut off at the token limit")
	// ErrNoAnswer means the response held no text.
	ErrNoAnswer = errors.New("claude: the response had no text")
)

// APIError is an error response from the API, reduced to what is safe to
// log: the SDK's own error text includes the response body.
type APIError struct {
	StatusCode int
	Type       string // e.g. rate_limit_error, overloaded_error
	RequestID  string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("claude: API error %d", e.StatusCode)
	if e.Type != "" {
		msg += " " + e.Type
	}
	if e.RequestID != "" {
		msg += " (request " + e.RequestID + ")"
	}
	return msg
}

// Client calls one model.
type Client struct {
	api   anthropic.Client
	model string
}

// New returns a client for model; an empty model means DefaultModel. opts are
// for tests, which point the client at a local server.
//
// Retries are off. The caller counts a call against a spend limit before
// making it, and that only bounds the spend if one counted call is one
// request.
func New(apiKey, model string, opts ...option.RequestOption) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("claude: no API key")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	options := append([]option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
	}, opts...)
	return &Client{api: anthropic.NewClient(options...), model: model}, nil
}

// Model returns the ID of the model the client calls.
func (c *Client) Model() string { return c.model }

// CompleteJSON sends one system prompt and one user message and returns the
// model's answer: a JSON document that follows schema, which the API
// enforces (structured outputs). The answer is still untrusted: the schema
// fixes its shape, not its values.
//
// The call is bounded by ctx; there is no streaming and no retry. Sampling
// parameters and a thinking budget are deliberately absent: current models
// reject them.
func (c *Client) CompleteJSON(ctx context.Context, system, user string, schema map[string]any) (string, error) {
	message, err := c.api.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
		OutputConfig: anthropic.OutputConfigParam{
			// The task is a simple choice; low effort keeps it quick and cheap.
			Effort: anthropic.OutputConfigEffortLow,
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	})
	if err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			return "", &APIError{StatusCode: apiErr.StatusCode, Type: string(apiErr.Type()), RequestID: apiErr.RequestID}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("claude: %w", ctxErr)
		}
		// A transport error: its text names the host, never the key or the body.
		return "", fmt.Errorf("claude: request failed: %w", err)
	}

	// Token counts only: what the call cost, not what it said.
	log.Printf("claude: model=%s stop=%s input_tokens=%d output_tokens=%d",
		c.model, message.StopReason, message.Usage.InputTokens, message.Usage.OutputTokens)

	switch message.StopReason {
	case anthropic.StopReasonRefusal:
		return "", ErrRefused
	case anthropic.StopReasonMaxTokens:
		return "", ErrTruncated
	}
	// The answer is the text; a response may open with thinking blocks.
	var text strings.Builder
	for _, block := range message.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(t.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return "", ErrNoAnswer
	}
	return text.String(), nil
}
