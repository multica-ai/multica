package googlechat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	webhookHost           = "chat.googleapis.com"
	webhookRequestTimeout = 10 * time.Second
)

var webhookPathPattern = regexp.MustCompile(`^/v1/spaces/([A-Za-z0-9_-]+)/messages$`)

// Sender posts rendered notification text to one Google Chat incoming webhook.
type Sender interface {
	Send(ctx context.Context, webhookURL, text string) error
}

// Client is the production Google Chat incoming-webhook sender.
type Client struct {
	httpClient *http.Client
}

// NewClient creates a sender with a fixed timeout, no proxy, and no redirects.
func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   webhookRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// ValidateWebhookURL accepts only Google Chat incoming-webhook URLs.
func ValidateWebhookURL(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("Google Chat webhook URL is invalid")
	}
	if endpoint.Scheme != "https" ||
		endpoint.Host != webhookHost ||
		endpoint.User != nil ||
		endpoint.Port() != "" ||
		endpoint.Fragment != "" {
		return nil, errors.New("Google Chat webhook must use https://chat.googleapis.com without a port, userinfo, or fragment")
	}
	if !webhookPathPattern.MatchString(endpoint.EscapedPath()) {
		return nil, errors.New("Google Chat webhook path must match /v1/spaces/{space}/messages")
	}
	query := endpoint.Query()
	if strings.TrimSpace(query.Get("key")) == "" || strings.TrimSpace(query.Get("token")) == "" {
		return nil, errors.New("Google Chat webhook must include non-empty key and token parameters")
	}
	return endpoint, nil
}

// MaskWebhookURL returns a safe display value that never exposes key or token.
func MaskWebhookURL(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	endpoint, err := ValidateWebhookURL(raw)
	if err != nil {
		return "https://chat.googleapis.com/v1/spaces/…/messages?key=••••&token=••••"
	}
	matches := webhookPathPattern.FindStringSubmatch(endpoint.EscapedPath())
	if len(matches) != 2 {
		return "https://chat.googleapis.com/v1/spaces/…/messages?key=••••&token=••••"
	}
	space := []rune(matches[1])
	if len(space) > 4 {
		space = space[:4]
	}
	return "https://chat.googleapis.com/v1/spaces/" + string(space) + "…/messages?key=••••&token=••••"
}

// SpaceID returns the non-secret space identifier for structured logs.
func SpaceID(raw string) string {
	endpoint, err := ValidateWebhookURL(raw)
	if err != nil {
		return ""
	}
	matches := webhookPathPattern.FindStringSubmatch(endpoint.EscapedPath())
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

// Send posts one text message. Errors intentionally omit the webhook URL.
func (c *Client) Send(ctx context.Context, webhookURL, text string) error {
	endpoint, err := ValidateWebhookURL(webhookURL)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return errors.New("could not encode Google Chat message")
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint.String(),
		bytes.NewReader(payload),
	)
	if err != nil {
		return errors.New("could not create Google Chat request")
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) && urlErr.Err != nil {
			return fmt.Errorf("Google Chat webhook request failed: %w", urlErr.Err)
		}
		return fmt.Errorf("Google Chat webhook request failed: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Google Chat webhook returned HTTP %d", response.StatusCode)
	}
	return nil
}
