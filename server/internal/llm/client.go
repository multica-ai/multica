package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const anthropicBaseURL = "https://api.anthropic.com/v1/messages"

// LLMClient wraps the Anthropic Messages API for single-turn text generation.
// Falls back gracefully when ANTHROPIC_API_KEY is not configured.
type LLMClient struct {
	apiKey string
	client *http.Client
}

// NewClient creates an LLMClient using ANTHROPIC_API_KEY from the environment.
func NewClient() *LLMClient {
	return &LLMClient{
		apiKey: os.Getenv("ANTHROPIC_API_KEY"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// IsConfigured returns true when an API key is available.
func (c *LLMClient) IsConfigured() bool {
	return c.apiKey != ""
}

// Generate sends a prompt to the Anthropic Messages API and returns the generated text.
// Returns an error when the API key is not set or the request fails.
func (c *LLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	if !c.IsConfigured() {
		return "", fmt.Errorf("ANTHROPIC_API_KEY not configured")
	}

	reqBody, err := json.Marshal(map[string]any{
		"model":      "claude-3-5-haiku-20241022",
		"max_tokens": 1024,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic API error: status %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from Anthropic API")
	}
	return result.Content[0].Text, nil
}
