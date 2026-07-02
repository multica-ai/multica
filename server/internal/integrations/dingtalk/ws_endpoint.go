package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// This file opens a DingTalk Stream connection: a POST to the gateway that
// returns a single-use WebSocket endpoint + ticket. It replaces the vendor SDK's
// client.Start handshake. The returned URL (endpoint with the ticket appended)
// is what the connector dials.

const (
	connectionsOpenPath = "/v1.0/gateway/connections/open"
	streamUserAgent     = "multica-dingtalk/1.0"
	openConnectTimeout  = 5 * time.Second
)

// streamSubscription is one {type, topic} entry in the open request. The chatbot
// connection subscribes to the two SYSTEM control topics plus the bot-message
// callback topic.
type streamSubscription struct {
	Type  string `json:"type"`
	Topic string `json:"topic"`
}

// chatbotSubscriptions is the fixed subscription set for a bot-message stream.
func chatbotSubscriptions() []streamSubscription {
	return []streamSubscription{
		{Type: frameTypeSystem, Topic: systemTopicPing},
		{Type: frameTypeSystem, Topic: systemTopicDisconnect},
		{Type: frameTypeCallback, Topic: botMessageTopic},
	}
}

type openConnectionRequest struct {
	ClientID      string               `json:"clientId"`
	ClientSecret  string               `json:"clientSecret"`
	Subscriptions []streamSubscription `json:"subscriptions"`
	UserAgent     string               `json:"ua"`
}

type openConnectionResponse struct {
	Endpoint string `json:"endpoint"`
	Ticket   string `json:"ticket"`
}

// openConnection registers a Stream connection and returns the dial-ready wss
// URL (endpoint?ticket=…). apiBase + httpClient come from the shared outbound
// Client so tests can point them at an httptest server.
func openConnection(ctx context.Context, httpClient *http.Client, apiBase, appKey, appSecret string) (string, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	reqBody, err := json.Marshal(openConnectionRequest{
		ClientID:      appKey,
		ClientSecret:  appSecret,
		Subscriptions: chatbotSubscriptions(),
		UserAgent:     streamUserAgent,
	})
	if err != nil {
		return "", fmt.Errorf("marshal open request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, openConnectTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+connectionsOpenPath, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build open request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("open connection: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("open connection: status %d: %s", resp.StatusCode, string(body))
	}
	var out openConnectionResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode open response: %w", err)
	}
	if out.Endpoint == "" || out.Ticket == "" {
		return "", fmt.Errorf("open connection: empty endpoint or ticket")
	}
	return fmt.Sprintf("%s?ticket=%s", out.Endpoint, url.QueryEscape(out.Ticket)), nil
}
