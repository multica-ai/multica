package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	omniRouteBaseURLKey = "OMNIROUTE_BASE_URL"
	omniRouteAPIKeyKey  = "OMNIROUTE_API_KEY"
)

type omnirouteBackend struct {
	cfg    Config
	client *http.Client
}

type omniRouteChatRequest struct {
	Model    string              `json:"model,omitempty"`
	Messages []omniRouteMessage  `json:"messages"`
	Stream   bool                `json:"stream"`
	Tools    []omniRouteToolSpec `json:"tools,omitempty"`
}

type omniRouteMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	ToolCalls  []omniRouteToolCall `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
}

type omniRouteToolSpec struct {
	Type     string                `json:"type"`
	Function omniRouteFunctionSpec `json:"function"`
}

type omniRouteFunctionSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type omniRouteToolCall struct {
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function omniRouteFunctionCall `json:"function"`
}

type omniRouteFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type omniRouteChatResponse struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Choices []omniRouteChoice `json:"choices"`
	Usage   *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		PromptDetails    *struct {
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
	} `json:"usage,omitempty"`
}

type omniRouteChoice struct {
	Message      omniRouteDelta `json:"message"`
	Delta        omniRouteDelta `json:"delta"`
	FinishReason string         `json:"finish_reason"`
}

type omniRouteDelta struct {
	Role      string                   `json:"role"`
	Content   string                   `json:"content"`
	ToolCalls []omniRouteToolCallDelta `json:"tool_calls"`
}

type omniRouteToolCallDelta struct {
	Index    int                    `json:"index"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function omniRouteFunctionDelta `json:"function"`
}

type omniRouteFunctionDelta struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type omniRouteConfig struct {
	BaseURL string
	APIKey  string
}

func (b *omnirouteBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	cfg, err := resolveOmniRouteConfig(b.cfg.Env)
	if err != nil {
		return nil, err
	}
	client := b.client
	if client == nil {
		client = http.DefaultClient
	}
	if b.cfg.Logger == nil {
		b.cfg.Logger = slog.Default()
	}
	if len(opts.McpConfig) > 0 {
		return b.executeWithTools(ctx, cfg, client, prompt, opts)
	}

	request := omniRouteChatRequest{
		Model: opts.Model,
		Messages: []omniRouteMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}
	if strings.TrimSpace(opts.SystemPrompt) != "" {
		request.Messages = append([]omniRouteMessage{{Role: "system", Content: opts.SystemPrompt}}, request.Messages...)
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("omniroute: encode request: %w", err)
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	httpReq, err := http.NewRequestWithContext(runCtx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omniroute: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if sessionID := strings.TrimSpace(opts.ResumeSessionID); sessionID != "" {
		httpReq.Header.Set("X-Session-Id", sessionID)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omniroute: request: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		defer resp.Body.Close()
		message := sanitizedHTTPError(resp.Body)
		cancel()
		return nil, fmt.Errorf("omniroute: upstream HTTP %d: %s", resp.StatusCode, message)
	}

	msgCh := make(chan Message, 128)
	resultCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer resp.Body.Close()
		defer close(msgCh)
		defer close(resultCh)
		start := time.Now()
		result := Result{Status: "completed"}
		var output strings.Builder
		var sessionID = opts.ResumeSessionID
		if headerSessionID := omniRouteSessionHeader(resp); headerSessionID != "" {
			sessionID = headerSessionID
		}
		if err := consumeOmniRouteSSE(resp.Body, msgCh, &output, &result, &sessionID, request.Model); err != nil {
			result.Status = statusForContext(runCtx, err)
			result.Error = err.Error()
		}
		if headerSessionID := omniRouteSessionHeader(resp); headerSessionID != "" {
			sessionID = headerSessionID
		}
		result.Output = output.String()
		result.SessionID = sessionID
		result.DurationMs = time.Since(start).Milliseconds()
		resultCh <- result
	}()
	return &Session{Messages: msgCh, Result: resultCh}, nil
}

type omniRouteTurn struct {
	output    string
	result    Result
	sessionID string
	calls     []omniRouteToolCall
	messages  []Message
}

func (b *omnirouteBackend) executeWithTools(ctx context.Context, cfg omniRouteConfig, client *http.Client, prompt string, opts ExecOptions) (*Session, error) {
	runCtx, cancel := runContext(ctx, opts.Timeout)
	registry, err := buildOmniRouteToolRegistry(runCtx, opts.McpConfig, client, opts.AllowedTools, opts.AllowedToolsConfigured)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("omniroute: load MCP tools: %w", err)
	}
	request := omniRouteChatRequest{Model: opts.Model, Stream: true, Tools: registry.tools, Messages: []omniRouteMessage{{Role: "user", Content: prompt}}}
	if strings.TrimSpace(opts.SystemPrompt) != "" {
		request.Messages = append([]omniRouteMessage{{Role: "system", Content: opts.SystemPrompt}}, request.Messages...)
	}
	msgCh := make(chan Message, 128)
	resultCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer registry.close()
		defer close(msgCh)
		defer close(resultCh)
		start := time.Now()
		result := Result{Status: "completed"}
		sessionID := opts.ResumeSessionID
		maxTurns := opts.MaxTurns
		if maxTurns <= 0 {
			maxTurns = 32
		}
		for turn := 0; turn < maxTurns; turn++ {
			current, turnErr := b.runOmniRouteTurn(runCtx, cfg, client, request, sessionID)
			if turnErr != nil {
				result.Status = statusForContext(runCtx, turnErr)
				result.Error = turnErr.Error()
				break
			}
			sessionID = current.sessionID
			for _, message := range current.messages {
				trySend(msgCh, message)
			}
			for model, usage := range current.result.Usage {
				result.Usage = map[string]TokenUsage{model: usage}
			}
			request.Messages = append(request.Messages, omniRouteMessage{Role: "assistant", Content: current.output, ToolCalls: current.calls})
			if len(current.calls) == 0 {
				result.Output = current.output
				break
			}
			for _, call := range current.calls {
				binding, ok := registry.bindings[call.Function.Name]
				args := map[string]interface{}{}
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					result.Status = "failed"
					result.Error = fmt.Sprintf("omniroute: invalid arguments for %q: %v", call.Function.Name, err)
					break
				}
				if !ok || !omniRouteToolAllowed(call.Function.Name, opts.AllowedTools) && opts.AllowedToolsConfigured {
					result.Status = "failed"
					result.Error = fmt.Sprintf("omniroute: tool %q is not allowed", call.Function.Name)
					break
				}
				toolOutput, callErr := binding.client.CallTool(runCtx, binding.name, args)
				if callErr != nil {
					toolOutput = "MCP error: " + callErr.Error()
				}
				trySend(msgCh, Message{Type: MessageToolResult, Tool: call.Function.Name, CallID: call.ID, Output: toolOutput})
				request.Messages = append(request.Messages, omniRouteMessage{Role: "tool", ToolCallID: call.ID, Name: call.Function.Name, Content: toolOutput})
			}
			if result.Status == "failed" {
				break
			}
		}
		if result.Status == "completed" && len(request.Messages) > 0 {
			if last := request.Messages[len(request.Messages)-1]; last.Role == "tool" {
				result.Status = "failed"
				result.Error = "omniroute: tool loop exhausted"
			}
		}
		result.SessionID = sessionID
		result.DurationMs = time.Since(start).Milliseconds()
		resultCh <- result
	}()
	return &Session{Messages: msgCh, Result: resultCh}, nil
}

func (b *omnirouteBackend) runOmniRouteTurn(ctx context.Context, cfg omniRouteConfig, client *http.Client, request omniRouteChatRequest, sessionID string) (omniRouteTurn, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return omniRouteTurn{}, fmt.Errorf("omniroute: encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return omniRouteTurn{}, fmt.Errorf("omniroute: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	if sessionID != "" {
		httpReq.Header.Set("X-Session-Id", sessionID)
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return omniRouteTurn{}, fmt.Errorf("omniroute: request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return omniRouteTurn{}, fmt.Errorf("omniroute: upstream HTTP %d: %s", resp.StatusCode, sanitizedHTTPError(resp.Body))
	}
	localMessages := make(chan Message, 128)
	var output strings.Builder
	result := Result{}
	newSession := sessionID
	if headerSessionID := omniRouteSessionHeader(resp); headerSessionID != "" {
		newSession = headerSessionID
	}
	if err := consumeOmniRouteSSE(resp.Body, localMessages, &output, &result, &newSession, request.Model); err != nil {
		return omniRouteTurn{}, err
	}
	if headerSessionID := omniRouteSessionHeader(resp); headerSessionID != "" {
		newSession = headerSessionID
	}
	close(localMessages)
	turn := omniRouteTurn{output: output.String(), result: result, sessionID: newSession}
	for message := range localMessages {
		turn.messages = append(turn.messages, message)
		if message.Type == MessageToolUse {
			args, _ := json.Marshal(message.Input)
			turn.calls = append(turn.calls, omniRouteToolCall{ID: message.CallID, Type: "function", Function: omniRouteFunctionCall{Name: message.Tool, Arguments: string(args)}})
		}
	}
	return turn, nil
}

func omniRouteSessionHeader(resp *http.Response) string {
	for _, name := range []string{"X-OmniRoute-Session-Id", "X-Session-Id"} {
		if value := strings.TrimSpace(resp.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func resolveOmniRouteConfig(env map[string]string) (omniRouteConfig, error) {
	value := func(key string) string {
		return strings.TrimSpace(env[key])
	}
	baseURL := strings.TrimRight(value(omniRouteBaseURLKey), "/")
	if baseURL == "" {
		return omniRouteConfig{}, errors.New("omniroute: OMNIROUTE_BASE_URL is not configured")
	}
	apiKey := value(omniRouteAPIKeyKey)
	if apiKey == "" {
		return omniRouteConfig{}, errors.New("omniroute: OMNIROUTE_API_KEY is not configured")
	}
	return omniRouteConfig{BaseURL: baseURL, APIKey: apiKey}, nil
}

func consumeOmniRouteSSE(r io.Reader, msgCh chan<- Message, output *strings.Builder, result *Result, sessionID *string, model string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 10*1024*1024)
	pendingCalls := make(map[int]*omniRouteToolCall)
	flushCalls := func() error {
		for _, call := range pendingCalls {
			args := map[string]interface{}{}
			if call.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					return fmt.Errorf("omniroute: invalid tool arguments for %q: %w", call.Function.Name, err)
				}
			}
			trySend(msgCh, Message{Type: MessageToolUse, Tool: call.Function.Name, CallID: call.ID, Input: args})
		}
		return nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return flushCalls()
		}
		var event omniRouteChatResponse
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("omniroute: decode SSE event: %w", err)
		}
		if event.ID != "" {
			*sessionID = event.ID
		}
		if event.Model != "" {
			model = event.Model
		}
		for _, choice := range event.Choices {
			delta := choice.Delta
			if delta.Content == "" && delta.Role == "" && len(delta.ToolCalls) == 0 {
				delta = choice.Message
			}
			if delta.Content != "" {
				output.WriteString(delta.Content)
				trySend(msgCh, Message{Type: MessageText, Content: delta.Content})
			}
			for _, fragment := range delta.ToolCalls {
				call := pendingCalls[fragment.Index]
				if call == nil {
					call = &omniRouteToolCall{Type: fragment.Type}
					pendingCalls[fragment.Index] = call
				}
				if fragment.ID != "" {
					call.ID = fragment.ID
				}
				if fragment.Function.Name != "" {
					call.Function.Name += fragment.Function.Name
				}
				call.Function.Arguments += fragment.Function.Arguments
			}
			if choice.FinishReason == "tool_calls" {
				if err := flushCalls(); err != nil {
					return err
				}
				pendingCalls = make(map[int]*omniRouteToolCall)
			}
		}
		if event.Usage != nil {
			usage := TokenUsage{InputTokens: event.Usage.PromptTokens, OutputTokens: event.Usage.CompletionTokens}
			if event.Usage.PromptDetails != nil {
				usage.CacheReadTokens = event.Usage.PromptDetails.CachedTokens
			}
			if model != "" {
				result.Usage = map[string]TokenUsage{model: usage}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("omniroute: read SSE stream: %w", err)
	}
	return flushCalls()
}

func sanitizedHTTPError(r io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	message := strings.TrimSpace(string(b))
	if message == "" {
		return "empty upstream response"
	}
	return strings.ReplaceAll(message, "Bearer", "[auth]")
}

func statusForContext(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	return "failed"
}
