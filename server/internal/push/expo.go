package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultExpoEndpoint = "https://exp.host/--/api/v2/push/send"

type Message struct {
	To       string         `json:"to"`
	Title    string         `json:"title"`
	Body     string         `json:"body,omitempty"`
	Sound    string         `json:"sound,omitempty"`
	Priority string         `json:"priority,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type Sender interface {
	Send(ctx context.Context, messages []Message) ([]string, error)
}

type ExpoClient struct {
	Endpoint    string
	AccessToken string
	HTTPClient  *http.Client
}

func NewExpoClientFromEnv() *ExpoClient {
	return &ExpoClient{
		Endpoint:    defaultExpoEndpoint,
		AccessToken: strings.TrimSpace(os.Getenv("EXPO_ACCESS_TOKEN")),
		HTTPClient:  &http.Client{Timeout: 10 * time.Second},
	}
}

type expoTicket struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type expoResponse struct {
	Data []expoTicket `json:"data"`
}

// Send delivers at most one Expo request. Callers keep batches below Expo's
// documented 100-message limit.
func (c *ExpoClient) Send(ctx context.Context, messages []Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	if len(messages) > 100 {
		return nil, fmt.Errorf("Expo push batch has %d messages; maximum is 100", len(messages))
	}

	body, err := json.Marshal(messages)
	if err != nil {
		return nil, fmt.Errorf("marshal Expo push request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build Expo push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.AccessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	}

	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Expo push request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("Expo push returned %s: %s", resp.Status, strings.TrimSpace(string(limited)))
	}

	var result expoResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode Expo push response: %w", err)
	}
	if len(result.Data) != len(messages) {
		return nil, fmt.Errorf("Expo push returned %d tickets for %d messages", len(result.Data), len(messages))
	}

	var invalid []string
	for i, ticket := range result.Data {
		if ticket.Status == "error" && ticket.Details.Error == "DeviceNotRegistered" {
			invalid = append(invalid, messages[i].To)
		}
	}
	return invalid, nil
}
