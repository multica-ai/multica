package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	pushoverMessagesURL = "https://api.pushover.net/1/messages.json"
	pushoverKeyLength   = 30
)

type PushoverService struct {
	applicationToken string
	messagesURL      string
	client           *http.Client
}

type pushoverResponse struct {
	Status  int      `json:"status"`
	Errors  []string `json:"errors"`
	Request string   `json:"request"`
}

func NewPushoverService() *PushoverService {
	return newPushoverService(
		strings.TrimSpace(os.Getenv("PUSHOVER_APPLICATION_TOKEN")),
		pushoverMessagesURL,
		&http.Client{Timeout: 10 * time.Second},
	)
}

func newPushoverService(applicationToken, messagesURL string, client *http.Client) *PushoverService {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &PushoverService{
		applicationToken: strings.TrimSpace(applicationToken),
		messagesURL:      messagesURL,
		client:           client,
	}
}

func IsValidPushoverKey(key string) bool {
	key = strings.TrimSpace(key)
	if len(key) != pushoverKeyLength {
		return false
	}
	for _, ch := range key {
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') {
			return false
		}
	}
	return true
}

func (s *PushoverService) Enabled() bool {
	return s != nil && IsValidPushoverKey(s.applicationToken)
}

func (s *PushoverService) SendLoginCode(ctx context.Context, userKey, code string) error {
	return s.sendNotification(ctx, userKey, "Multica Login Code", code)
}

func (s *PushoverService) SendTestNotification(ctx context.Context, userKey string) error {
	return s.sendNotification(
		ctx,
		userKey,
		"Multica Test Notification",
		"You are now setup to receive Pushover notifications via Multica.",
	)
}

func (s *PushoverService) sendNotification(ctx context.Context, userKey, title, message string) error {
	if !s.Enabled() {
		return errors.New("pushover is not configured")
	}
	userKey = strings.TrimSpace(userKey)
	if !IsValidPushoverKey(userKey) {
		return errors.New("invalid Pushover user key")
	}

	form := url.Values{
		"token":   {s.applicationToken},
		"user":    {userKey},
		"title":   {title},
		"message": {message},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.messagesURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create Pushover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("send Pushover request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return fmt.Errorf("read Pushover response: %w", err)
	}
	var result pushoverResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode Pushover response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || result.Status != 1 {
		detail := strings.Join(result.Errors, "; ")
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("Pushover rejected message (HTTP %d, request %s): %s", resp.StatusCode, result.Request, detail)
	}
	return nil
}
