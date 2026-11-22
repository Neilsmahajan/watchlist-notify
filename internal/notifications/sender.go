package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type EmailSender interface {
	SendEmail(toEmail, subject, htmlBody string) error
}

type Sender struct {
	postmarkServerAPIToken string
	baseURL                string
	fromEmail              string
	messageStream          string
	httpClient             *http.Client
}

type EmailBody struct {
	From          string `json:"From"`
	To            string `json:"To"`
	Subject       string `json:"Subject"`
	HtmlBody      string `json:"HtmlBody"`
	MessageStream string `json:"MessageStream"`
}

const (
	DefaultBaseURL       = "https://api.postmarkapp.com/email"
	DefaultFromEmail     = "alerts@watchlistnotify.com"
	DefaultMessageStream = "outbound"
)

func New(fromEmail, messageStream string) (*Sender, error) {
	postmarkServerAPIToken := os.Getenv("POSTMARK_SERVER_TOKEN")
	if strings.TrimSpace(postmarkServerAPIToken) == "" {
		return nil, errors.New("POSTMARK_SERVER_TOKEN environment variable not set")
	}

	baseURL := os.Getenv("POSTMARK_BASE_URL")
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}

	if strings.TrimSpace(fromEmail) == "" {
		fromEmail = DefaultFromEmail
	}

	if strings.TrimSpace(messageStream) == "" {
		messageStream = DefaultMessageStream
	}

	return &Sender{
		postmarkServerAPIToken: postmarkServerAPIToken,
		baseURL:                strings.TrimRight(baseURL, "/"),
		fromEmail:              fromEmail,
		messageStream:          messageStream,
		httpClient: &http.Client{
			Timeout: 7 * time.Second,
		},
	}, nil
}

// RejectedError means Postmark answered and refused the message, so nothing
// was sent.
//
// It carries the HTTP status and Postmark's numeric ErrorCode and nothing
// else from the response. Postmark's error messages quote the addresses
// involved ("... inactive recipient: someone@example.com"), and this error
// ends up in logs, so the response text is never kept.
type RejectedError struct {
	StatusCode int
	// ErrorCode is Postmark's API error code (for example 300 for an invalid
	// email request, 406 for an inactive recipient), or 0 when the response
	// did not carry one.
	ErrorCode int
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("postmark rejected the email: status %d, error code %d", e.StatusCode, e.ErrorCode)
}

// postmarkErrorCode reads the ErrorCode out of a Postmark error response and
// discards the rest. Anything that is not the expected JSON yields 0.
func postmarkErrorCode(body io.Reader) int {
	var parsed struct {
		ErrorCode int `json:"ErrorCode"`
	}
	// Error responses are a short JSON object; cap what is read.
	if err := json.NewDecoder(io.LimitReader(body, 4096)).Decode(&parsed); err != nil {
		return 0
	}
	return parsed.ErrorCode
}

// IsDefiniteFailure reports whether err from SendEmail proves that no email
// went out: Postmark refused it with a client error, or the connection was
// never established. Anything else (a timeout, a dropped connection, a 5xx)
// leaves the outcome unknown, and callers that must not send twice have to
// assume the email may have been delivered.
func IsDefiniteFailure(err error) bool {
	if err == nil {
		return false
	}
	var rejected *RejectedError
	if errors.As(err, &rejected) {
		return rejected.StatusCode >= 400 && rejected.StatusCode < 500
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// SendEmail sends one HTML email through Postmark. A non-200 answer is
// returned as a *RejectedError. No error it returns contains the recipient's
// address or any of Postmark's response text.
func (s *Sender) SendEmail(toEmail, subject, htmlBody string) error {
	body := EmailBody{
		From:          s.fromEmail,
		To:            toEmail,
		Subject:       subject,
		HtmlBody:      htmlBody,
		MessageStream: s.messageStream,
	}
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, s.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Postmark-Server-Token", s.postmarkServerAPIToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return &RejectedError{StatusCode: resp.StatusCode, ErrorCode: postmarkErrorCode(resp.Body)}
	}
	return nil
}
