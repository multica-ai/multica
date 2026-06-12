package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/firtalgateway"
)

type GatewayMessage struct {
	Role          string                  `json:"role"`
	Content       string                  `json:"content,omitempty"`
	ContentBlocks []AnthropicContentBlock `json:"-"`
	ToolCalls     []GatewayToolCall       `json:"tool_calls,omitempty"`
	ToolCallID    string                  `json:"tool_call_id,omitempty"`
}

// gatewayOpenAIContentPart is one element of an OpenAI Chat Completions
// multimodal content array: a text part or an image_url part.
type gatewayOpenAIContentPart struct {
	Type     string                 `json:"type"`
	Text     string                 `json:"text,omitempty"`
	ImageURL *gatewayOpenAIImageURL `json:"image_url,omitempty"`
}

type gatewayOpenAIImageURL struct {
	URL string `json:"url"`
}

// MarshalJSON renders the message for the OpenAI-compat
// `/v1/chat/completions` wire. When the message carries ContentBlocks (image
// and text attachments folded in for the compat path), they are emitted as a
// multimodal content array — text blocks become text parts and base64 image
// blocks become image_url data-URL parts — so vision input survives the compat
// transport instead of being dropped by the `json:"-"` tag. Messages without
// ContentBlocks keep the plain string content. The Anthropic-native path does
// not use this method; it marshals AnthropicMessage instead.
func (m GatewayMessage) MarshalJSON() ([]byte, error) {
	type wire struct {
		Role       string            `json:"role"`
		Content    any               `json:"content,omitempty"`
		ToolCalls  []GatewayToolCall `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
	}
	w := wire{Role: m.Role, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}
	if parts := gatewayContentBlocksToOpenAIParts(m.ContentBlocks); len(parts) > 0 {
		w.Content = parts
	} else if m.Content != "" {
		w.Content = m.Content
	}
	return json.Marshal(w)
}

// gatewayContentBlocksToOpenAIParts converts Anthropic content blocks into
// OpenAI multimodal content parts. Text blocks become text parts; base64 image
// blocks become image_url data-URL parts. Document and other non-text/image
// blocks are skipped — the executor folds PDFs to text before serialization, so
// a surviving non-image block has no compat representation. Returns nil when
// there is nothing to emit, so the caller falls back to the string content.
func gatewayContentBlocksToOpenAIParts(blocks []AnthropicContentBlock) []gatewayOpenAIContentPart {
	if len(blocks) == 0 {
		return nil
	}
	parts := make([]gatewayOpenAIContentPart, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, gatewayOpenAIContentPart{Type: "text", Text: b.Text})
			}
		case "image":
			if b.Source != nil && b.Source.Type == "base64" && b.Source.MediaType != "" && b.Source.Data != "" {
				url := fmt.Sprintf("data:%s;base64,%s", b.Source.MediaType, b.Source.Data)
				parts = append(parts, gatewayOpenAIContentPart{Type: "image_url", ImageURL: &gatewayOpenAIImageURL{URL: url}})
			}
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
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
	// ToolResultChars is the total characters of tool-result content produced
	// across the tool loop. It feeds the prune_tool_results cost measurement
	// (FIR-2325); it is summed alongside Usage and is 0 for tool-free runs.
	ToolResultChars int64
	// PrunedToolResultChars is the characters of superseded tool-result content
	// actually dropped from the transcript mid-run when the prune_tool_results
	// saving is "on" (FIR-2325). 0 when the saving is off/shadow or when no round
	// was ever superseded. This is the *measured* saving, as opposed to
	// ToolResultChars which is the would-save estimate. It counts each pruned
	// character ONCE — see PrunedContextCharsCompounded for the real saving.
	PrunedToolResultChars int64
	// PrunedContextCharsCompounded is the COMPOUNDING saving from pruning: each
	// character pruned at round i would otherwise have been resent in every model
	// call after round i (the rest of this run), so it is counted once per
	// subsequent call, not once total. Pruning early in a run therefore saves more
	// than pruning in the last round. This is the honest measure of context the
	// run avoided sending — PrunedToolResultChars undercounts it (FIR-2639). It is
	// always >= PrunedToolResultChars (every pruned round is followed by at least
	// the final model call).
	PrunedContextCharsCompounded int64
	// PromptInputChars is the total characters of prompt content actually sent to
	// the model across every request in the run (the full transcript on each
	// round, summed). Paired with Usage.InputTokens (+ cache tokens) it yields a
	// real per-run chars-per-token ratio for THIS model, so prune_tool_results
	// can report an accurate saved-token count without a holdout control arm
	// (FIR-2572). 0 when nothing was sent / not instrumented.
	PromptInputChars int64
}

// GatewayRequestMeta is the run context forwarded to the registry AI-gateway on
// every call so each logged request carries who/what/which-issue (not bare
// UUIDs) and, when CaptureContent is set, the full prompt + tool calls +
// response. Free-text fields (names, titles) are URL-encoded into headers so
// non-ASCII (Danish æøå, emoji) survives the latin-1 HTTP header transport; the
// registry decodes them with decodeURIComponent.
type GatewayRequestMeta struct {
	TaskID          string
	AgentID         string
	AgentName       string
	WorkspaceID     string
	IssueID         string
	IssueTitle      string
	ProjectID       string
	ProjectName     string
	TriggerUserID   string
	TriggerUserName string
	Surface         string // chat | issue | autopilot
	AutopilotID     string
	// CaptureContent asks the registry to persist the full prompt/response/tool
	// content for this call (not just token counts + a hash).
	CaptureContent bool
}

// applyContextHeaders attaches the run identity + content-capture signal to a
// gateway request. ID/enum fields go verbatim (ASCII); human-readable
// free-text fields are URL-encoded so arbitrary Unicode survives HTTP headers.
func (m GatewayRequestMeta) applyContextHeaders(req *http.Request) {
	setRaw := func(key, val string) {
		if val != "" {
			req.Header.Set(key, val)
		}
	}
	setText := func(key, val string) {
		if val != "" {
			req.Header.Set(key, url.QueryEscape(val))
		}
	}
	setRaw("X-Session-ID", m.TaskID)
	setRaw("X-Agent-ID", m.AgentID)
	// X-User-ID stays the agent UUID for backward-compatible session/skill
	// attribution; the human who triggered the run is X-Trigger-User-ID.
	setRaw("X-User-ID", m.AgentID)
	setText("X-Agent-Name", m.AgentName)
	setRaw("X-Workspace-ID", m.WorkspaceID)
	setRaw("X-Issue-ID", m.IssueID)
	setText("X-Issue-Title", m.IssueTitle)
	setRaw("X-Project-ID", m.ProjectID)
	setText("X-Project-Name", m.ProjectName)
	setRaw("X-Trigger-User-ID", m.TriggerUserID)
	setText("X-Trigger-User-Name", m.TriggerUserName)
	setRaw("X-Surface", m.Surface)
	setRaw("X-Autopilot-ID", m.AutopilotID)
	if m.CaptureContent {
		req.Header.Set("X-Capture-Content", "full")
	}
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
	if c.cfg.EntryMode == FirtalGatewayEntryModeNative {
		return c.completeNativeMessages(ctx, model, messages, meta)
	}
	return c.CompleteWithTools(ctx, model, messages, nil, meta)
}

func (c *GatewayClient) completeNativeMessages(ctx context.Context, model string, messages []GatewayMessage, meta GatewayRequestMeta) (GatewayCompletion, error) {
	systemText := ""
	msgStart := 0
	if len(messages) > 0 && messages[0].Role == "system" {
		systemText = messages[0].Content
		msgStart = 1
	}
	return c.completeAnthropic(ctx, model, systemText, ConvertGatewayMessagesToAnthropic(messages[msgStart:]), nil, nil, meta)
}

// CompleteWithTools is Complete with optional OpenAI-compatible tool
// definitions. When tools is non-empty, the gateway may respond with
// `tool_calls` instead of (or alongside) `content`; both branches are returned
// in the GatewayCompletion. Callers (the executor's tool-loop) decide whether
// to dispatch the calls and run another iteration, or treat the text as final.
func (c *GatewayClient) CompleteWithTools(ctx context.Context, model string, messages []GatewayMessage, tools []GatewayToolDef, meta GatewayRequestMeta) (GatewayCompletion, error) {
	return c.completeChat(ctx, model, messages, tools, "", meta)
}

// CompleteForcingText is the tool-loop's forced-final call: it keeps the tool
// definitions attached but sets tool_choice="none" so the model must answer
// with text instead of emitting another tool call. Keeping `tools` attached
// (rather than dropping it) is what keeps the request valid for Anthropic,
// whose API rejects a transcript that references tool_use/tool_result blocks
// when the request omits the tools that define them — the root cause of the
// FIR-2421 HTTP 400. tool_choice="none" then guarantees the loop terminates
// with text rather than risking yet another tool call.
func (c *GatewayClient) CompleteForcingText(ctx context.Context, model string, messages []GatewayMessage, tools []GatewayToolDef, meta GatewayRequestMeta) (GatewayCompletion, error) {
	return c.completeChat(ctx, model, messages, tools, "none", meta)
}

// completeChat is the shared OpenAI-compat /v1/chat/completions call. toolChoice
// is forwarded as the request's `tool_choice` field when non-empty (the gateway
// translates "none"/"auto"/"required" to the Anthropic equivalent).
func (c *GatewayClient) completeChat(ctx context.Context, model string, messages []GatewayMessage, tools []GatewayToolDef, toolChoice string, meta GatewayRequestMeta) (GatewayCompletion, error) {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		model = c.cfg.Model
	}
	if model == "" {
		model = defaultFirtalGatewayModel
	}
	baseURL, err := firtalgateway.NormalizeTrustedBaseURL(c.cfg.BaseURL)
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
	if toolChoice != "" {
		body["tool_choice"] = toolChoice
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
	meta.applyContextHeaders(req)

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
