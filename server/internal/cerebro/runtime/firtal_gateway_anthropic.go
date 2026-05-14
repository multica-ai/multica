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

// AnthropicCacheControl is the cache_control block understood by the Anthropic
// messages API. Only type="ephemeral" is meaningful for prompt caching.
type AnthropicCacheControl struct {
	Type string `json:"type"`
}

// AnthropicSystemBlock is a single block in the system array for the
// Anthropic messages request. Adding cache_control on the last block enables
// prompt caching for the system prompt and all preceding conversation turns.
type AnthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicTool is the Anthropic-native tool definition. Adding cache_control
// on the last tool in the array caches the tool list alongside the system
// prompt so repeat calls don't re-tokenize the schema definitions.
type AnthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  map[string]any         `json:"input_schema"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// AnthropicContentBlock supports the three content block types used in the
// Anthropic messages protocol:
//   - type=text: Text field holds the plain-text content.
//   - type=tool_use: ID/Name/Input hold the tool invocation.
//   - type=tool_result: ToolUseID and Content hold the result of a tool call.
type AnthropicContentBlock struct {
	Type      string                  `json:"type"`
	Text      string                  `json:"text,omitempty"`
	ID        string                  `json:"id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Input     map[string]any          `json:"input,omitempty"`
	ToolUseID string                  `json:"tool_use_id,omitempty"`
	Content   []AnthropicContentBlock `json:"content,omitempty"`
}

// AnthropicMessage is a single turn in the Anthropic messages conversation.
// Content may be a plain string (for simple user messages) or a slice of
// AnthropicContentBlock (for assistant tool-use turns and tool-result turns).
type AnthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string | []AnthropicContentBlock
}

// anthropicRequest is the JSON body sent to /api/ai/proxy/v1/messages.
type anthropicRequest struct {
	Model     string                 `json:"model"`
	MaxTokens int                    `json:"max_tokens"`
	System    []AnthropicSystemBlock `json:"system,omitempty"`
	Messages  []AnthropicMessage     `json:"messages"`
	Tools     []AnthropicTool        `json:"tools,omitempty"`
}

// anthropicResponse is the parsed response from /api/ai/proxy/v1/messages.
type anthropicResponse struct {
	ID      string                  `json:"id"`
	Type    string                  `json:"type"`
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
	Model   string                  `json:"model"`
	Usage   *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	// Firtal gateway may include a top-level cost rollup.
	Firtal *struct {
		InputTokens       int64 `json:"input_tokens"`
		OutputTokens      int64 `json:"output_tokens"`
		CachedInputTokens int64 `json:"cached_input_tokens"`
		CostCents         int64 `json:"cost_cents"`
	} `json:"firtal"`
}

// CompleteAnthropicWithTools calls the Anthropic-native /v1/messages endpoint
// through the Firtal gateway proxy. It builds system blocks with cache_control
// on the last block, optionally attaches tool definitions (cache_control on
// last tool), sends the request, and converts the response back to the
// GatewayCompletion shape shared with the OpenAI-compat path.
func (c *GatewayClient) CompleteAnthropicWithTools(
	ctx context.Context,
	model string,
	systemText string,
	messages []AnthropicMessage,
	tools []AnthropicTool,
	meta GatewayRequestMeta,
) (GatewayCompletion, error) {
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

	// Build system array. Cache the last (and only) block so the system
	// prompt + all tool definitions are eligible for prompt caching.
	system := []AnthropicSystemBlock{
		{
			Type:         "text",
			Text:         systemText,
			CacheControl: &AnthropicCacheControl{Type: "ephemeral"},
		},
	}

	// Add cache_control to the last tool so the tool list is cached too.
	toolsCopy := make([]AnthropicTool, len(tools))
	copy(toolsCopy, tools)
	if len(toolsCopy) > 0 {
		last := &toolsCopy[len(toolsCopy)-1]
		last.CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
	}

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: c.cfg.MaxTokens,
		System:    system,
		Messages:  messages,
		Tools:     toolsCopy,
	}

	raw, err := json.Marshal(reqBody)
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/ai/proxy/v1/messages", bytes.NewReader(raw))
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("build anthropic request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Skill", "multica-server-runtime")
	req.Header.Set("X-Tags", "multica,cerebro,server-runtime,anthropic")
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
		return GatewayCompletion{}, fmt.Errorf("call anthropic gateway: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return GatewayCompletion{}, fmt.Errorf("read anthropic response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return GatewayCompletion{}, fmt.Errorf("anthropic gateway returned HTTP %d: %s", resp.StatusCode, truncateGatewayError(string(respBody), 2048))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return GatewayCompletion{}, fmt.Errorf("parse anthropic response: %w", err)
	}

	// Extract text and tool_use blocks from the content array.
	var textParts []string
	var toolCalls []GatewayToolCall
	for _, block := range parsed.Content {
		switch block.Type {
		case "text":
			if t := strings.TrimSpace(block.Text); t != "" {
				textParts = append(textParts, t)
			}
		case "tool_use":
			if strings.TrimSpace(block.Name) == "" {
				continue
			}
			// Marshal the input map back to a JSON string so it matches the
			// GatewayToolCall.Function.Arguments shape used by the dispatcher.
			argsJSON, err := json.Marshal(block.Input)
			if err != nil {
				argsJSON = []byte("{}")
			}
			toolCalls = append(toolCalls, GatewayToolCall{
				ID:   block.ID,
				Type: "function",
				Function: GatewayToolCallFunction{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	output := strings.Join(textParts, "")
	if output == "" && len(toolCalls) == 0 {
		return GatewayCompletion{Model: model, Usage: usageFromAnthropicResponse(parsed)}, fmt.Errorf("anthropic gateway returned no assistant content")
	}

	return GatewayCompletion{
		Model:     model,
		Output:    output,
		ToolCalls: toolCalls,
		Usage:     usageFromAnthropicResponse(parsed),
	}, nil
}

func usageFromAnthropicResponse(resp anthropicResponse) GatewayUsage {
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
	return GatewayUsage{
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
	}
}

// ConvertGatewayMessagesToAnthropic converts the OpenAI-compatible
// GatewayMessage slice (produced by buildGatewayIssueMessages etc.) into the
// Anthropic messages format. System messages are dropped (the caller passes
// systemText separately). Tool result messages (role=tool) become user turns
// with tool_result content blocks.
func ConvertGatewayMessagesToAnthropic(messages []GatewayMessage) []AnthropicMessage {
	out := make([]AnthropicMessage, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "system":
			// Consumed as the system parameter; skip here.
			continue

		case "user":
			if m.ToolCallID != "" {
				// tool result turn
				out = append(out, AnthropicMessage{
					Role: "user",
					Content: []AnthropicContentBlock{
						{
							Type:      "tool_result",
							ToolUseID: m.ToolCallID,
							Content: []AnthropicContentBlock{
								{Type: "text", Text: m.Content},
							},
						},
					},
				})
			} else {
				out = append(out, AnthropicMessage{
					Role:    "user",
					Content: m.Content,
				})
			}

		case "assistant":
			if len(m.ToolCalls) > 0 {
				blocks := make([]AnthropicContentBlock, 0, len(m.ToolCalls)+1)
				if m.Content != "" {
					blocks = append(blocks, AnthropicContentBlock{
						Type: "text",
						Text: m.Content,
					})
				}
				for _, tc := range m.ToolCalls {
					var input map[string]any
					if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" {
						_ = json.Unmarshal([]byte(raw), &input)
					}
					if input == nil {
						input = map[string]any{}
					}
					blocks = append(blocks, AnthropicContentBlock{
						Type:  "tool_use",
						ID:    tc.ID,
						Name:  tc.Function.Name,
						Input: input,
					})
				}
				out = append(out, AnthropicMessage{
					Role:    "assistant",
					Content: blocks,
				})
			} else {
				out = append(out, AnthropicMessage{
					Role:    "assistant",
					Content: m.Content,
				})
			}

		case "tool":
			// OpenAI-compat tool results have role="tool" with ToolCallID.
			out = append(out, AnthropicMessage{
				Role: "user",
				Content: []AnthropicContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: m.ToolCallID,
						Content: []AnthropicContentBlock{
							{Type: "text", Text: m.Content},
						},
					},
				},
			})
		}
	}
	return out
}
