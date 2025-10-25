package notifications

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	DefaultFromEmail     = "contact@watchlistnotify.com"
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

	req, err := http.NewRequest("POST", s.baseURL, bytes.NewBuffer(jsonBody))
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
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Warning: Failed to close response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to send email, status: %s, and failed to read body: %v", resp.Status, err)
		}
		return fmt.Errorf("failed to send email, status: %s, body: %s", resp.Status, string(bodyBytes))
	}

	return nil
}
