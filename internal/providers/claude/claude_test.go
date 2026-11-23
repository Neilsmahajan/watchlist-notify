package claude

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// These tests run the real SDK against a local server standing in for the
// Messages API. Nothing here reaches Anthropic.

const testKey = "sk-ant-test-0000"

var testSchema = map[string]any{
	"type":                 "object",
	"properties":           map[string]any{"ok": map[string]any{"type": "boolean"}},
	"required":             []string{"ok"},
	"additionalProperties": false,
}

// message is a Messages API response body with the given content blocks.
func message(stopReason string, blocks ...string) string {
	return `{"id":"msg_test","type":"message","role":"assistant","model":"claude-haiku-5-5",` +
		`"content":[` + strings.Join(blocks, ",") + `],"stop_reason":"` + stopReason + `","stop_sequence":null,` +
		`"usage":{"input_tokens":1200,"output_tokens":90}}`
}

func textBlock(text string) string {
	encoded, _ := json.Marshal(text)
	return `{"type":"text","text":` + string(encoded) + `}`
}

const thinkingBlock = `{"type":"thinking","thinking":"Weighing the options.","signature":"sig"}`

// fakeAPI answers every request with status and body, and records what it
// was sent.
type fakeAPI struct {
	server   *httptest.Server
	requests atomic.Int32
	path     string
	header   http.Header
	body     map[string]any
}

func newFakeAPI(t *testing.T, status int, body string) *fakeAPI {
	t.Helper()
	f := &fakeAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests.Add(1)
		f.path, f.header = r.URL.Path, r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &f.body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_test_1")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeAPI) client(t *testing.T, model string) *Client {
	t.Helper()
	c, err := New(testKey, model, option.WithBaseURL(f.server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewNeedsAKey(t *testing.T) {
	for _, key := range []string{"", "   "} {
		if c, err := New(key, ""); err == nil || c != nil {
			t.Errorf("New(%q) = %v, %v; want an error", key, c, err)
		}
	}
}

func TestNewDefaultsTheModel(t *testing.T) {
	c, err := New(testKey, "  ")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Model() != DefaultModel {
		t.Errorf("model = %q, want %q", c.Model(), DefaultModel)
	}
	c, _ = New(testKey, "claude-sonnet-5-5")
	if c.Model() != "claude-sonnet-5-5" {
		t.Errorf("model = %q, want the one asked for", c.Model())
	}
}

func TestCompleteJSONRequest(t *testing.T) {
	api := newFakeAPI(t, http.StatusOK, message("end_turn", textBlock(`{"ok":true}`)))
	got, err := api.client(t, "").CompleteJSON(context.Background(), "the system prompt", "the user message", testSchema)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if got != `{"ok":true}` {
		t.Errorf("answer = %q", got)
	}

	if api.path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", api.path)
	}
	// This version of the SDK sends an API key as a bearer credential.
	if got := api.header.Get("Authorization"); got != "Bearer "+testKey {
		t.Errorf("Authorization = %q, want the configured key as a bearer credential", got)
	}
	body := api.body
	if body["model"] != DefaultModel {
		t.Errorf("model = %v, want %s", body["model"], DefaultModel)
	}
	if body["max_tokens"] != float64(maxTokens) {
		t.Errorf("max_tokens = %v, want %d", body["max_tokens"], maxTokens)
	}

	wantSystem := []any{map[string]any{"type": "text", "text": "the system prompt"}}
	if !reflect.DeepEqual(body["system"], wantSystem) {
		t.Errorf("system = %v, want %v", body["system"], wantSystem)
	}
	wantMessages := []any{map[string]any{
		"role":    "user",
		"content": []any{map[string]any{"type": "text", "text": "the user message"}},
	}}
	if !reflect.DeepEqual(body["messages"], wantMessages) {
		t.Errorf("messages = %v, want %v", body["messages"], wantMessages)
	}

	// Structured output: the schema travels in output_config.format.
	wantOutput := map[string]any{
		"effort": "low",
		"format": map[string]any{
			"type": "json_schema",
			"schema": map[string]any{
				"type":                 "object",
				"properties":           map[string]any{"ok": map[string]any{"type": "boolean"}},
				"required":             []any{"ok"},
				"additionalProperties": false,
			},
		},
	}
	if !reflect.DeepEqual(body["output_config"], wantOutput) {
		t.Errorf("output_config = %v, want %v", body["output_config"], wantOutput)
	}

	// Parameters current models reject, and features this call must not use.
	for _, key := range []string{"temperature", "top_p", "top_k", "thinking", "stream", "tools"} {
		if _, present := body[key]; present {
			t.Errorf("request sets %q: %v", key, body[key])
		}
	}
}

func TestCompleteJSONSkipsThinkingBlocks(t *testing.T) {
	api := newFakeAPI(t, http.StatusOK, message("end_turn", thinkingBlock, textBlock(`{"ok":`), textBlock(`false}`)))
	got, err := api.client(t, "").CompleteJSON(context.Background(), "s", "u", testSchema)
	if err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if got != `{"ok":false}` {
		t.Errorf("answer = %q, want the text blocks only, joined", got)
	}
}

func TestCompleteJSONStopReasons(t *testing.T) {
	cases := []struct {
		name string
		body string
		want error
	}{
		{"refusal", message("refusal"), ErrRefused},
		// A cut-off answer is half a JSON document, however it starts.
		{"max_tokens", message("max_tokens", textBlock(`{"ok":tr`)), ErrTruncated},
		{"only thinking", message("end_turn", thinkingBlock), ErrNoAnswer},
		{"no content", message("end_turn"), ErrNoAnswer},
		{"blank text", message("end_turn", textBlock("  \n")), ErrNoAnswer},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeAPI(t, http.StatusOK, tc.body)
			got, err := api.client(t, "").CompleteJSON(context.Background(), "s", "u", testSchema)
			if !errors.Is(err, tc.want) || got != "" {
				t.Errorf("CompleteJSON = %q, %v; want %v", got, err, tc.want)
			}
		})
	}
}

// An error from the API is reported with its status and type, is not
// retried, and does not carry the response body into the logs.
func TestCompleteJSONAPIError(t *testing.T) {
	cases := []struct {
		status int
		kind   string
	}{
		{http.StatusTooManyRequests, "rate_limit_error"},
		{529, "overloaded_error"},
		{http.StatusInternalServerError, "api_error"},
		{http.StatusUnauthorized, "authentication_error"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			api := newFakeAPI(t, tc.status,
				`{"type":"error","error":{"type":"`+tc.kind+`","message":"BODY-TEXT-THAT-MUST-NOT-BE-LOGGED"}}`)
			_, err := api.client(t, "").CompleteJSON(context.Background(), "s", "MOOD-TEXT", testSchema)

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v (%T), want an *APIError", err, err)
			}
			if apiErr.StatusCode != tc.status || apiErr.Type != tc.kind || apiErr.RequestID != "req_test_1" {
				t.Errorf("error = %+v, want status %d, type %s, request req_test_1", apiErr, tc.status, tc.kind)
			}
			for _, secret := range []string{"BODY-TEXT", "MOOD-TEXT", testKey} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error text %q contains %q", err.Error(), secret)
				}
			}
			if got := api.requests.Load(); got != 1 {
				t.Errorf("%d requests were made, want 1: a counted call must be one request", got)
			}
		})
	}
}

func TestCompleteJSONHonoursTheDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	c, err := New(testKey, "", option.WithBaseURL(server.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.CompleteJSON(ctx, "s", "u", testSchema)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the call took %s with a 50ms deadline", elapsed)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("error text %q contains the key", err.Error())
	}
}

func TestCompleteJSONTransportError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	url := server.URL
	server.Close() // nothing listens there any more

	c, err := New(testKey, "", option.WithBaseURL(url))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.CompleteJSON(context.Background(), "s", "MOOD-TEXT", testSchema)
	if err == nil {
		t.Fatal("no error from a server that is not there")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("a transport error was reported as an API error: %v", err)
	}
	for _, secret := range []string{"MOOD-TEXT", testKey} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error text %q contains %q", err.Error(), secret)
		}
	}
}
