package agent

// CEREBRO-PATCH(agent-openai-eu): TECH-2989 EU-compliant OpenAI backend via api.eu.openai.com (OpenAI EU Data Residency).
// Config: OPENAI_EU_API_KEY (required), OPENAI_EU_ENDPOINT (optional, default https://api.eu.openai.com/v1).

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

const (
	openaiEUProvider        = "openai-eu"
	openaiEUDefaultEndpoint = "https://api.eu.openai.com/v1"
	openaiEUDefaultModel    = "gpt-4o"
)

type openaiEUBackend struct {
	cfg Config
}

type openaiEUConfig struct {
	Endpoint string
	APIKey   string
	Model    string
}

type openaiEUChatRequest struct {
	Model    string           `json:"model"`
	Messages []openaiEUMsg    `json:"messages"`
	Stream   bool             `json:"stream"`
	// MaxTokens is omitted for o-series reasoning models (they use max_completion_tokens).
	MaxTokens *int `json:"max_tokens,omitempty"`
}

type openaiEUMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiEUResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (b *openaiEUBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 20 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	msgCh := make(chan Message, 16)
	resCh := make(chan Result, 1)

	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		start := time.Now()
		cfg, err := b.config(opts)
		if err != nil {
			resCh <- Result{Status: "failed", Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
			return
		}

		output, usage, err := b.callOpenAIEU(runCtx, cfg, prompt, opts)
		if err != nil {
			resCh <- Result{Status: "failed", Error: err.Error(), DurationMs: time.Since(start).Milliseconds()}
			return
		}
		if output != "" {
			trySend(msgCh, Message{Type: MessageText, Content: output})
		}
		resCh <- Result{
			Status:     "completed",
			Output:     output,
			DurationMs: time.Since(start).Milliseconds(),
			Usage:      map[string]TokenUsage{cfg.Model: usage},
		}
	}()

	return &Session{Messages: msgCh, Result: resCh}, nil
}

func (b *openaiEUBackend) config(opts ExecOptions) (openaiEUConfig, error) {
	endpoint := strings.TrimRight(os.Getenv("OPENAI_EU_ENDPOINT"), "/")
	if endpoint == "" {
		endpoint = openaiEUDefaultEndpoint
	}
	apiKey := os.Getenv("OPENAI_EU_API_KEY")
	if apiKey == "" {
		return openaiEUConfig{}, fmt.Errorf("OPENAI_EU_API_KEY is required for the openai-eu provider")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = openaiEUDefaultModel
	}
	return openaiEUConfig{Endpoint: endpoint, APIKey: apiKey, Model: model}, nil
}

// isOSeries returns true for o1, o1-mini and o3-mini which have restricted API surfaces:
// no system role, no temperature, no max_tokens (use max_completion_tokens).
func isOSeries(model string) bool {
	return model == "o1" || model == "o1-mini" || model == "o3-mini"
}

func (b *openaiEUBackend) callOpenAIEU(ctx context.Context, cfg openaiEUConfig, prompt string, opts ExecOptions) (string, TokenUsage, error) {
	system := strings.TrimSpace(opts.SystemPrompt)
	oSeries := isOSeries(cfg.Model)

	var messages []openaiEUMsg
	if system != "" && !oSeries {
		messages = append(messages, openaiEUMsg{Role: "system", Content: system})
		messages = append(messages, openaiEUMsg{Role: "user", Content: prompt})
	} else if system != "" && oSeries {
		// o-series models don't support system role; prepend system to user message.
		messages = append(messages, openaiEUMsg{Role: "user", Content: system + "\n\n" + prompt})
	} else {
		messages = append(messages, openaiEUMsg{Role: "user", Content: prompt})
	}

	reqBody := openaiEUChatRequest{
		Model:    cfg.Model,
		Messages: messages,
		Stream:   false,
	}
	if !oSeries {
		maxTok := 4096
		reqBody.MaxTokens = &maxTok
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("marshal openai-eu request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("build openai-eu request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("call openai-eu: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("read openai-eu response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", TokenUsage{}, fmt.Errorf("openai-eu returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 2048))
	}

	var parsed openaiEUResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", TokenUsage{}, fmt.Errorf("parse openai-eu response: %w", err)
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", TokenUsage{}, fmt.Errorf("openai-eu error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", TokenUsage{}, fmt.Errorf("openai-eu returned no choices")
	}

	output := strings.TrimSpace(parsed.Choices[0].Message.Content)
	var usage TokenUsage
	if parsed.Usage != nil {
		usage = TokenUsage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
		}
	}
	return output, usage, nil
}

// openaiEUStaticModels returns the EU-routed OpenAI chat models available via
// api.eu.openai.com (OpenAI EU Data Residency). Text-embedding models are
// available via the semantic search provider (CEREBRO_SEARCH_SEMANTIC_ENDPOINT
// set to https://api.eu.openai.com/v1/embeddings).
func openaiEUStaticModels() []Model {
	return []Model{
		{ID: "gpt-4o", Label: "GPT-4o (EU)", Provider: "openai", Default: true},
		{ID: "gpt-4o-mini", Label: "GPT-4o mini (EU)", Provider: "openai"},
		{ID: "gpt-4-turbo", Label: "GPT-4 Turbo (EU)", Provider: "openai"},
		{ID: "o1", Label: "o1 (EU)", Provider: "openai"},
		{ID: "o1-mini", Label: "o1-mini (EU)", Provider: "openai"},
		{ID: "o3-mini", Label: "o3-mini (EU)", Provider: "openai"},
	}
}
