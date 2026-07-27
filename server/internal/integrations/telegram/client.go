package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// BotInfo represents basic information about a Telegram bot.
type BotInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// ClientOption is a functional option for configuring a Client.
type ClientOption func(*Client)

// Client is a minimal Telegram Bot API client.
type Client struct {
	botToken   string
	apiBase    string
	httpClient *http.Client
}

// NewClient creates a new Telegram Bot API client.
func NewClient(botToken string, opts ...ClientOption) *Client {
	c := &Client{
		botToken:   botToken,
		apiBase:    "https://api.telegram.org",
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithAPIBase returns an option to set a custom API base URL.
func WithAPIBase(base string) ClientOption {
	return func(c *Client) {
		c.apiBase = base
	}
}

// WithHTTPClient returns an option to set a custom HTTP client.
func WithHTTPClient(client *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = client
	}
}

// call makes a request to the Telegram Bot API.
func (c *Client) call(ctx context.Context, method string, req any, out any) error {
	// Marshal request body
	reqBody, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// Construct URL
	url := fmt.Sprintf("%s/bot%s/%s", c.apiBase, c.botToken, method)

	// Create request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	// Decode response envelope
	var envelope map[string]any
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		description, _ := envelope["description"].(string)
		return fmt.Errorf("telegram api error: %s", description)
	}

	// Check ok flag
	ok, _ := envelope["ok"].(bool)
	if !ok {
		description, _ := envelope["description"].(string)
		return fmt.Errorf("telegram api error: %s", description)
	}

	// Decode result
	if out != nil {
		resultData, ok := envelope["result"]
		if ok {
			resultBytes, err := json.Marshal(resultData)
			if err != nil {
				return fmt.Errorf("marshal result: %w", err)
			}
			if err := json.Unmarshal(resultBytes, out); err != nil {
				return fmt.Errorf("decode result: %w", err)
			}
		}
	}

	return nil
}

// GetMe retrieves basic information about the bot.
func (c *Client) GetMe(ctx context.Context) (BotInfo, error) {
	var info BotInfo
	if err := c.call(ctx, "getMe", map[string]any{}, &info); err != nil {
		return BotInfo{}, err
	}
	return info, nil
}

// SetWebhook sets the webhook URL for receiving updates.
func (c *Client) SetWebhook(ctx context.Context, url, secretToken string) error {
	req := map[string]any{
		"url":             url,
		"secret_token":    secretToken,
		"allowed_updates": []string{"message"},
	}
	return c.call(ctx, "setWebhook", req, nil)
}

// DeleteWebhook removes the webhook URL.
func (c *Client) DeleteWebhook(ctx context.Context) error {
	return c.call(ctx, "deleteWebhook", map[string]any{}, nil)
}

// SendMessage sends a message to a chat.
func (c *Client) SendMessage(ctx context.Context, chatID string, text string, threadID string) error {
	req := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if threadID != "" {
		req["message_thread_id"] = threadID
	}
	return c.call(ctx, "sendMessage", req, nil)
}
