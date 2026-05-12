package agent

// CEREBRO-PATCH(agent-firtal-gateway-runtime): managed Data Registry AI Gateway backend.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	firtalGatewayProvider     = "firtal-gateway"
	firtalGatewayDefaultModel = "claude-sonnet-4-6"
)

type firtalGatewayBackend struct {
	cfg Config
}

type firtalGatewayConfig struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature *float64
}

type firtalGatewayResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails *struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		CachedInputTokens int64 `json:"cached_input_tokens"`
	} `json:"usage"`
	Firtal *struct {
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		CachedInputTokens int64 `json:"cached_input_tokens"`
		CostCents         int64 `json:"cost_cents"`
	} `json:"firtal"`
}

type firtalGatewayModelsResponse struct {
	Data []struct {
		ID                string `json:"id"`
		OwnedBy           string `json:"owned_by"`
		FirtalDisplayName string `json:"firtal_display_name"`
	} `json:"data"`
}

func (b *firtalGatewayBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
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

		output, usage, err := b.callGateway(runCtx, cfg, prompt, opts)
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

func (b *firtalGatewayBackend) config(opts ExecOptions) (firtalGatewayConfig, error) {
	baseURL := strings.TrimRight(firstEnv(b.cfg.Env,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL",
		"FIRTAL_AE_GATEWAY_URL",
		"FIRTAL_DATA_REGISTRY_URL",
	), "/")
	apiKey := firstEnv(b.cfg.Env,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY",
		"FIRTAL_AE_GATEWAY_KEY",
		"FIRTAL_DATA_REGISTRY_API_KEY",
	)
	if baseURL == "" {
		return firtalGatewayConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL is required")
	}
	if apiKey == "" {
		return firtalGatewayConfig{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY is required")
	}

	model := strings.TrimSpace(opts.Model)
	if model == "" || model == "auto" {
		model = firstEnv(b.cfg.Env, "FIRTAL_DATA_REGISTRY_AI_MODEL", "FIRTAL_AE_GATEWAY_MODEL")
	}
	if model == "" {
		model = firtalGatewayDefaultModel
	}

	maxTokens := 4096
	if raw := firstEnv(b.cfg.Env, "FIRTAL_DATA_REGISTRY_AI_MAX_TOKENS", "FIRTAL_AE_GATEWAY_MAX_TOKENS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	var temperature *float64
	if raw := firstEnv(b.cfg.Env, "FIRTAL_DATA_REGISTRY_AI_TEMPERATURE", "FIRTAL_AE_GATEWAY_TEMPERATURE"); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
			temperature = &parsed
		}
	}

	return firtalGatewayConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		Model:       model,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}, nil
}

func (b *firtalGatewayBackend) callGateway(ctx context.Context, cfg firtalGatewayConfig, prompt string, opts ExecOptions) (string, TokenUsage, error) {
	system := strings.TrimSpace(strings.Join([]string{
		"You are a managed Multica chat assistant for Firtal. Answer directly and do not claim access to local tools, files, shells, or repositories.",
		strings.TrimSpace(opts.SystemPrompt),
	}, "\n\n"))

	body := map[string]any{
		"model":      cfg.Model,
		"max_tokens": cfg.MaxTokens,
		"stream":     false,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": prompt},
		},
	}
	if cfg.Temperature != nil {
		body["temperature"] = *cfg.Temperature
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("marshal gateway request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/api/ai/proxy/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("build gateway request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Skill", "multica-managed-runtime")
	req.Header.Set("X-Tags", "multica,cerebro,managed-runtime")
	if taskID := b.cfg.Env["MULTICA_TASK_ID"]; taskID != "" {
		req.Header.Set("X-Session-ID", taskID)
	}
	if userID := b.cfg.Env["MULTICA_AGENT_ID"]; userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("call gateway: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", TokenUsage{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", TokenUsage{}, fmt.Errorf("gateway returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 2048))
	}

	var parsed firtalGatewayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", TokenUsage{}, fmt.Errorf("parse gateway response: %w", err)
	}

	output := strings.TrimSpace(extractGatewayContent(parsed))
	if output == "" {
		return "", usageFromGatewayResponse(parsed), fmt.Errorf("gateway returned no assistant content")
	}

	return output, usageFromGatewayResponse(parsed), nil
}

func extractGatewayContent(resp firtalGatewayResponse) string {
	if len(resp.Choices) == 0 {
		return ""
	}
	content := resp.Choices[0].Message.Content
	switch v := content.(type) {
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func usageFromGatewayResponse(resp firtalGatewayResponse) TokenUsage {
	if resp.Firtal != nil {
		return TokenUsage{
			InputTokens:     resp.Firtal.InputTokens,
			OutputTokens:    resp.Firtal.OutputTokens,
			CacheReadTokens: resp.Firtal.CachedInputTokens,
			CostCents:       resp.Firtal.CostCents,
		}
	}
	if resp.Usage == nil {
		return TokenUsage{}
	}
	cached := resp.Usage.CachedInputTokens
	if cached == 0 && resp.Usage.PromptTokensDetails != nil {
		cached = resp.Usage.PromptTokensDetails.CachedTokens
	}
	input := resp.Usage.InputTokens
	if input == 0 {
		input = resp.Usage.PromptTokens
	}
	output := resp.Usage.OutputTokens
	if output == 0 {
		output = resp.Usage.CompletionTokens
	}
	return TokenUsage{
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cached,
	}
}

func firstEnv(extra map[string]string, keys ...string) string {
	for _, key := range keys {
		if extra != nil {
			if v := strings.TrimSpace(extra[key]); v != "" {
				return v
			}
		}
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func truncateForError(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// CEREBRO-PATCH(agent-firtal-gateway-runtime): JEH-757 drop static fallback
// model list — the managed gateway is the single source of truth for which
// models are reachable.
//
// discoverFirtalGatewayModels fetches the live model catalog from the
// Data Registry AI Gateway. The gateway is the single source of truth
// for which models are reachable — when discovery cannot answer (URL
// or API key not configured, transport error, non-2xx response, parse
// failure), we return an empty list and the underlying error so the
// caller can decide whether to surface it. We deliberately do not ship
// a hard-coded fallback list, because a parallel static catalog drifts
// from the gateway's reality and silently masks misconfiguration.
func discoverFirtalGatewayModels(ctx context.Context) ([]Model, error) {
	baseURL := strings.TrimRight(firstEnv(nil,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL",
		"FIRTAL_AE_GATEWAY_URL",
		"FIRTAL_DATA_REGISTRY_URL",
	), "/")
	apiKey := firstEnv(nil,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY",
		"FIRTAL_AE_GATEWAY_KEY",
		"FIRTAL_DATA_REGISTRY_API_KEY",
	)
	if baseURL == "" {
		return []Model{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL is required")
	}
	if apiKey == "" {
		return []Model{}, fmt.Errorf("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY is required")
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, http.MethodGet, baseURL+"/api/ai/proxy/v1/models", nil)
	if err != nil {
		return []Model{}, fmt.Errorf("build gateway models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return []Model{}, fmt.Errorf("call gateway models: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return []Model{}, fmt.Errorf("read gateway models response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return []Model{}, fmt.Errorf("gateway models returned HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody), 2048))
	}

	var parsed firtalGatewayModelsResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return []Model{}, fmt.Errorf("parse gateway models response: %w", err)
	}

	models := make([]Model, 0, len(parsed.Data))
	for _, entry := range parsed.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(entry.FirtalDisplayName)
		if label == "" {
			label = id
		}
		provider := strings.TrimSpace(entry.OwnedBy)
		models = append(models, Model{
			ID:       id,
			Label:    label,
			Provider: provider,
			Default:  id == firtalGatewayDefaultModel,
		})
	}
	if len(models) == 0 {
		return models, nil
	}
	hasDefault := false
	for _, model := range models {
		if model.Default {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		models[0].Default = true
	}
	return models, nil
}
