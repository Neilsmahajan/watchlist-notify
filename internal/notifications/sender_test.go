package notifications

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestSender(t *testing.T, handler http.HandlerFunc) *Sender {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("POSTMARK_SERVER_TOKEN", "test-token")
	t.Setenv("POSTMARK_BASE_URL", srv.URL)
	sender, err := New("", "")
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func TestSendEmail(t *testing.T) {
	var got string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Postmark-Server-Token") != "test-token" {
			t.Error("request is missing the server token")
		}
		body, _ := io.ReadAll(r.Body)
		got = string(body)
		_, _ = w.Write([]byte(`{"ErrorCode":0}`))
	})

	if err := sender.SendEmail("to@example.com", "Hello", "<p>Hi</p>"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"To":"to@example.com"`, `"Subject":"Hello"`, `"From":"` + DefaultFromEmail + `"`, `"MessageStream":"outbound"`} {
		if !strings.Contains(got, want) {
			t.Errorf("request body %s does not contain %s", got, want)
		}
	}
}

func TestSendEmailRejected(t *testing.T) {
	sender := newTestSender(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"ErrorCode":406,"Message":"inactive recipient"}`, http.StatusUnprocessableEntity)
	})

	err := sender.SendEmail("to@example.com", "Hello", "<p>Hi</p>")
	var rejected *RejectedError
	if !errors.As(err, &rejected) || rejected.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("SendEmail() error = %v, want a 422 RejectedError", err)
	}
	if !strings.Contains(rejected.Body, "inactive recipient") {
		t.Errorf("RejectedError.Body = %q", rejected.Body)
	}
	if !IsDefiniteFailure(err) {
		t.Error("a 422 from Postmark was not classified as a definite failure")
	}
}

func TestIsDefiniteFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rejected with 401", &RejectedError{StatusCode: 401}, true},
		{"rejected with 422", fmt.Errorf("send: %w", &RejectedError{StatusCode: 422}), true},
		{"server error may have accepted the message", &RejectedError{StatusCode: 500}, false},
		{"gateway timeout", &RejectedError{StatusCode: 504}, false},
		{"dns failure", &net.DNSError{Err: "no such host", Name: "api.postmarkapp.com"}, true},
		{"connection refused", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"connection reset mid-request", &net.OpError{Op: "read", Err: errors.New("connection reset by peer")}, false},
		{"timeout", errors.New("net/http: timeout awaiting response headers"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDefiniteFailure(tt.err); got != tt.want {
				t.Errorf("IsDefiniteFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestNewRequiresToken(t *testing.T) {
	t.Setenv("POSTMARK_SERVER_TOKEN", "")
	if _, err := New("", ""); err == nil {
		t.Error("New succeeded without POSTMARK_SERVER_TOKEN")
	}
}
