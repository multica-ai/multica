package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// priorityFromSeverity maps inbox severity levels to ntfy priority numbers.
// ntfy priorities: 1=min, 2=low, 3=default, 4=high, 5=urgent
var priorityFromSeverity = map[string]int{
	"action_required": 5,
	"attention":       3,
	"info":            1,
}

// Message is the payload sent to a ntfy topic.
type Message struct {
	Title    string
	Body     string
	Severity string // "action_required", "attention", or "info"
	ClickURL string // deep-link back to the issue (X-Click header)
}

// Sender dispatches push notifications to ntfy topics.
type Sender struct {
	client *http.Client
}

// New creates a Sender with a 5-second HTTP timeout.
func New() *Sender {
	return &Sender{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Send posts a notification to topicURL. When token is non-empty it is sent
// as a Bearer auth header. Returns an error only for transport or HTTP-level
// failures; callers typically fire-and-forget via a goroutine.
func (s *Sender) Send(ctx context.Context, topicURL, token string, msg Message) error {
	priority, ok := priorityFromSeverity[msg.Severity]
	if !ok {
		priority = 3
	}

	body, err := json.Marshal(map[string]any{
		"topic":    "", // unused when posting directly to a topic URL
		"title":    msg.Title,
		"message":  msg.Body,
		"priority": priority,
	})
	if err != nil {
		return fmt.Errorf("ntfy: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, topicURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("ntfy: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if msg.ClickURL != "" {
		req.Header.Set("X-Click", msg.ClickURL)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy: server returned %d", resp.StatusCode)
	}
	return nil
}
