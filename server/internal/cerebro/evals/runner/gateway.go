package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// gateway.go is the live Completer: it calls the Firtal AI Gateway's
// Anthropic-compatible messages endpoint (POST /v1/messages, the same endpoint
// the workspace connection exposes as firtal_ai_gateway__post_v1_messages).
// It is constructed from the connection's base URL and key; the eval handler
// wires it in. The grader and prompt target depend only on the Completer
// interface, never on this wire format.

const (
	gatewayMessagesPath = "/v1/messages"
	gatewayMaxTokens    = 1024
	gatewayTimeout      = 60 * time.Second
)

// httpDoer is the minimal slice of *http.Client the completer needs, so tests
// can inject a stub without a live server.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// GatewayCompleter speaks the Anthropic messages API through the Firtal Gateway.
type GatewayCompleter struct {
	baseURL      string
	apiKey       string
	defaultModel string
	client       httpDoer
}

// NewGatewayCompleter builds the live completer. baseURL is the gateway root
// (the connection base URL), apiKey its key, and defaultModel the model used
// when a request does not override it. A nil client defaults to a timed
// *http.Client.
func NewGatewayCompleter(baseURL, apiKey, defaultModel string, client httpDoer) (*GatewayCompleter, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("gateway completer needs a base URL")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("gateway completer needs an API key")
	}
	if strings.TrimSpace(defaultModel) == "" {
		return nil, errors.New("gateway completer needs a default model")
	}
	if client == nil {
		client = &http.Client{Timeout: gatewayTimeout}
	}
	return &GatewayCompleter{
		baseURL:      baseURL,
		apiKey:       apiKey,
		defaultModel: defaultModel,
		client:       client,
	}, nil
}

type gatewayMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type gatewayRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	System    string           `json:"system,omitempty"`
	Messages  []gatewayMessage `json:"messages"`
}

type gatewayResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Complete sends one user turn to the gateway and returns the concatenated text
// blocks. CostCents is 0: the gateway does not return a per-call price, and the
// runner captures latency separately — pricing attribution is a later step.
func (g *GatewayCompleter) Complete(ctx context.Context, req CompletionRequest) (CompletionResult, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = g.defaultModel
	}
	payload := gatewayRequest{
		Model:     model,
		MaxTokens: gatewayMaxTokens,
		System:    req.System,
		Messages:  []gatewayMessage{{Role: "user", Content: req.Prompt}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CompletionResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+gatewayMessagesPath, bytes.NewReader(body))
	if err != nil {
		return CompletionResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpReq.Header.Set("x-api-key", g.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return CompletionResult{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return CompletionResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResult{}, fmt.Errorf("gateway returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var parsed gatewayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return CompletionResult{}, fmt.Errorf("gateway response parse: %w", err)
	}
	var text strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return CompletionResult{Text: text.String()}, nil
}
