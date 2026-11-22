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
type RejectedError struct {
	StatusCode int
	Body       string
}

func (e *RejectedError) Error() string {
	return fmt.Sprintf("postmark rejected the email: status %d: %s", e.StatusCode, e.Body)
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
// returned as a *RejectedError.
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
		// Postmark error bodies are a short JSON object; cap what is kept.
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return &RejectedError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(bodyBytes))}
	}
	return nil
}
