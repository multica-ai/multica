package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/firtalgateway"
)

type GatewayMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []GatewayToolCall `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

// GatewayToolDef is the OpenAI-compatible tool definition the gateway accepts
// on `/v1/chat/completions`. Only `type: "function"` is supported today.
type GatewayToolDef struct {
	Type     string              `json:"type"`
	Function GatewayToolFunction `json:"function"`
}

type GatewayToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// GatewayToolCall is the OpenAI-compatible tool invocation returned in
// `choices[0].message.tool_calls`. Function arguments arrive as a JSON-encoded
// string; the executor unmarshals into a map[string]any before dispatching.
type GatewayToolCall struct {
	ID       string                  `json:"id"`
	Type     string                  `json:"type"`
	Function GatewayToolCallFunction `json:"function"`
}

type GatewayToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type GatewayUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostCents        int64
}

type GatewayCompletion struct {
	Model     string
	Output    string
	ToolCalls []GatewayToolCall
	Usage     GatewayUsage
}

type GatewayRequestMeta struct {
	TaskID      string
	AgentID     string
	WorkspaceID string
}

type GatewayClient struct {
	cfg        FirtalGatewayRuntimeConfig
	httpClient *http.Client
}

func NewGatewayClient(cfg FirtalGatewayRuntimeConfig, httpClient *http.Client) *GatewayClient {
	if httpClient == nil {
		httpClient = firtalgateway.NewHTTPClient()
	}
	cfg = withFirtalGatewayDefaults(cfg)
	return &GatewayClient{cfg: cfg, httpClient: httpClient}
}

func (c *GatewayClient) Complete(ctx context.Context, model string, messages []GatewayMessage, meta GatewayRequestMeta) (GatewayCompletion, error) {
	return c.CompleteWithTools(ctx, model, messages, nil, meta)
}

// CompleteWithTools is Complete with optional OpenAI-compatible tool
// definitions. When tools is non-empty, the gateway may respond with
// `tool_calls` instead of (or alongside) `content`; both branches are returned
// in the GatewayCompletion. Callers (the executor's tool-loop) decide whether
// to dispatch the calls and run another iteration, or treat the text as final.
func (c *GatewayClient) CompleteWithTools(ctx context.Context, model string, messages []GatewayMessage, tools []GatewayToolDef, meta GatewayRequestMeta) (GatewayCompletion, error) {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		model = c.cfg.Model
	}
	if model == "" {
		model = defaultFirtalGatewayModel
	}
	baseURL, err := firtalgateway.NormalizeBaseURL(c.cfg.BaseURL)
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("invalid gateway URL: %w", err)
	}

	body := map[string]any{
		"model":      model,
		"max_tokens": c.cfg.MaxTokens,
		"stream":     false,
		"messages":   messages,
	}
	if c.cfg.Temperature != nil {
		body["temperature"] = *c.cfg.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("marshal gateway request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/ai/proxy/v1/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("build gateway request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Skill", "multica-server-runtime")
	req.Header.Set("X-Tags", "multica,cerebro,server-runtime")
	if meta.TaskID != "" {
		req.Header.Set("X-Session-ID", meta.TaskID)
	}
	if meta.AgentID != "" {
		req.Header.Set("X-User-ID", meta.AgentID)
	}
	if meta.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", meta.WorkspaceID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("call gateway: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("read gateway response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GatewayCompletion{}, fmt.Errorf("gateway returned HTTP %d: %s", resp.StatusCode, truncateGatewayError(string(respBody), 2048))
	}

	var parsed gatewayResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return GatewayCompletion{}, fmt.Errorf("parse gateway response: %w", err)
	}

	output := strings.TrimSpace(extractGatewayContent(parsed))
	toolCalls := extractGatewayToolCalls(parsed)
	if output == "" && len(toolCalls) == 0 {
		return GatewayCompletion{Model: model, Usage: usageFromGatewayResponse(parsed)}, fmt.Errorf("gateway returned no assistant content")
	}

	return GatewayCompletion{
		Model:     model,
		Output:    output,
		ToolCalls: toolCalls,
		Usage:     usageFromGatewayResponse(parsed),
	}, nil
}

type gatewayResponse struct {
	Choices []struct {
		Message struct {
			Content   any               `json:"content"`
			ToolCalls []GatewayToolCall `json:"tool_calls"`
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

func extractGatewayToolCalls(resp gatewayResponse) []GatewayToolCall {
	if len(resp.Choices) == 0 {
		return nil
	}
	calls := resp.Choices[0].Message.ToolCalls
	if len(calls) == 0 {
		return nil
	}
	out := make([]GatewayToolCall, 0, len(calls))
	for _, c := range calls {
		if strings.TrimSpace(c.Function.Name) == "" {
			continue
		}
		if c.Type == "" {
			c.Type = "function"
		}
		out = append(out, c)
	}
	return out
}

func extractGatewayContent(resp gatewayResponse) string {
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

func usageFromGatewayResponse(resp gatewayResponse) GatewayUsage {
	if resp.Firtal != nil {
		return GatewayUsage{
			InputTokens:     resp.Firtal.InputTokens,
			OutputTokens:    resp.Firtal.OutputTokens,
			CacheReadTokens: resp.Firtal.CachedInputTokens,
			CostCents:       resp.Firtal.CostCents,
		}
	}
	if resp.Usage == nil {
		return GatewayUsage{}
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
	return GatewayUsage{
		InputTokens:     input,
		OutputTokens:    output,
		CacheReadTokens: cached,
	}
}

func truncateGatewayError(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}
