package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (b *openAICompatibleBackend) doResponsesStreamingRequest(ctx context.Context, model string, messages []apiMessage, tools map[string]*apiHTTPMCPTool, msgCh chan<- Message) (apiCompletionResponse, error) {
	payload := map[string]any{
		"model":  model,
		"input":  responsesInput(messages),
		"stream": true,
	}
	if len(tools) > 0 {
		definitions := make([]map[string]any, 0, len(tools))
		for name, tool := range tools {
			definitions = append(definitions, map[string]any{
				"type":        "function",
				"name":        name,
				"description": tool.Description,
				"parameters":  tool.InputSchema,
			})
		}
		payload["tools"] = definitions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apiCompletionResponse{}, fmt.Errorf("encode Responses request: %w", err)
	}
	endpoint, err := providerProtocolEndpoint(b.cfg.APIBaseURL, "/responses")
	if err != nil {
		return apiCompletionResponse{}, err
	}
	resp, err := b.doProviderStreamRequest(ctx, endpoint, body, nil)
	if err != nil {
		return apiCompletionResponse{}, err
	}
	defer resp.Body.Close()
	if err := checkProviderHTTPResponse(resp, b.cfg.APIKey); err != nil {
		return apiCompletionResponse{}, err
	}

	var result apiCompletionResponse
	toolCalls := map[string]*apiToolCall{}
	toolOrder := []string{}
	done := false
	var eventName string
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if strings.TrimSpace(data) == "[DONE]" {
			done = true
			return nil
		}
		var event responsesStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("decode Responses stream event: %w", err)
		}
		typeName := strings.TrimSpace(event.Type)
		if typeName == "" {
			typeName = eventName
		}
		switch typeName {
		case "response.output_text.delta":
			content := sanitizeProviderOutput(event.Delta, b.cfg.APIKey)
			if content != "" {
				result.Text += content
				trySend(msgCh, Message{Type: MessageText, Content: content})
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
			reasoning := sanitizeProviderOutput(event.Delta, b.cfg.APIKey)
			if reasoning != "" {
				trySend(msgCh, Message{Type: MessageThinking, Content: reasoning})
			}
		case "response.output_item.added", "response.output_item.done":
			if err := addResponsesToolCall(toolCalls, &toolOrder, event.Item); err != nil {
				return err
			}
		case "response.function_call_arguments.delta":
			key := strings.TrimSpace(event.ItemID)
			if key == "" {
				key = strings.TrimSpace(event.CallID)
			}
			call := ensureResponsesToolCall(toolCalls, &toolOrder, key)
			call.Function.Arguments += event.Delta
		case "response.completed", "response.done":
			mergeResponsesCompletion(&result, toolCalls, &toolOrder, event.Response)
			done = true
		case "error", "response.error":
			message := event.Error.Message
			if message == "" {
				message = "provider returned a Responses stream error"
			}
			return fmt.Errorf("Responses stream error: %s", sanitizeProviderOutput(message, b.cfg.APIKey))
		}
		if event.Usage != nil {
			result.Usage = tokenUsageFromAPI(*event.Usage)
		}
		return nil
	}
	if err := scanProviderSSE(resp.Body, func(name string, data []string) error {
		eventName = name
		dataLines = append(dataLines[:0], data...)
		return flush()
	}, func() bool { return done }); err != nil {
		return apiCompletionResponse{}, err
	}
	if !done {
		return apiCompletionResponse{}, errors.New("Responses stream ended before a terminal response event")
	}
	for _, key := range toolOrder {
		if call := toolCalls[key]; call != nil {
			result.ToolCalls = append(result.ToolCalls, *call)
		}
	}
	return result, nil
}

type responsesStreamEvent struct {
	Type     string              `json:"type"`
	Delta    string              `json:"delta"`
	ItemID   string              `json:"item_id"`
	CallID   string              `json:"call_id"`
	Item     responsesOutputItem `json:"item"`
	Response responsesResponse   `json:"response"`
	Usage    *apiUsage           `json:"usage,omitempty"`
	Error    struct {
		Message string `json:"message"`
	} `json:"error"`
}

type responsesResponse struct {
	Usage  *apiUsage             `json:"usage,omitempty"`
	Output []responsesOutputItem `json:"output,omitempty"`
}

type responsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func responsesInput(messages []apiMessage) []any {
	input := make([]any, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			if text, ok := message.Content.(string); ok && text != "" {
				input = append(input, map[string]any{"role": "assistant", "content": text})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type":      "function_call",
					"id":        call.ID,
					"call_id":   call.ID,
					"name":      call.Function.Name,
					"arguments": call.Function.Arguments,
				})
			}
		case "tool":
			input = append(input, map[string]any{
				"type":    "function_call_output",
				"call_id": message.ToolCallID,
				"output":  message.Content,
			})
		default:
			input = append(input, map[string]any{"role": message.Role, "content": message.Content})
		}
	}
	return input
}

func addResponsesToolCall(calls map[string]*apiToolCall, order *[]string, item responsesOutputItem) error {
	if item.Type != "function_call" {
		return nil
	}
	key := strings.TrimSpace(item.CallID)
	if key == "" {
		key = strings.TrimSpace(item.ID)
	}
	call := ensureResponsesToolCall(calls, order, key)
	if call.ID == "" {
		call.ID = key
	}
	if call.Type == "" {
		call.Type = "function"
	}
	if call.Function.Name == "" {
		call.Function.Name = item.Name
	}
	if item.Arguments != "" {
		call.Function.Arguments = item.Arguments
	}
	return nil
}

func ensureResponsesToolCall(calls map[string]*apiToolCall, order *[]string, key string) *apiToolCall {
	if key == "" {
		key = fmt.Sprintf("responses-call-%d", len(*order)+1)
	}
	if call := calls[key]; call != nil {
		return call
	}
	call := &apiToolCall{ID: key, Type: "function"}
	calls[key] = call
	*order = append(*order, key)
	return call
}

func mergeResponsesCompletion(result *apiCompletionResponse, calls map[string]*apiToolCall, order *[]string, response responsesResponse) {
	if response.Usage != nil {
		result.Usage = tokenUsageFromAPI(*response.Usage)
	}
	for _, item := range response.Output {
		_ = addResponsesToolCall(calls, order, item)
	}
}

func (b *openAICompatibleBackend) doAnthropicStreamingRequest(ctx context.Context, model string, messages []apiMessage, tools map[string]*apiHTTPMCPTool, msgCh chan<- Message) (apiCompletionResponse, error) {
	payload := map[string]any{
		"model":      model,
		"messages":   anthropicMessages(messages),
		"max_tokens": 8192,
		"stream":     true,
	}
	for _, message := range messages {
		if message.Role == "system" {
			if text, ok := message.Content.(string); ok && strings.TrimSpace(text) != "" {
				payload["system"] = text
			}
			break
		}
	}
	if len(tools) > 0 {
		definitions := make([]map[string]any, 0, len(tools))
		for name, tool := range tools {
			var schema any
			if len(bytes.TrimSpace(tool.InputSchema)) > 0 {
				_ = json.Unmarshal(tool.InputSchema, &schema)
			}
			definitions = append(definitions, map[string]any{"name": name, "description": tool.Description, "input_schema": schema})
		}
		payload["tools"] = definitions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apiCompletionResponse{}, fmt.Errorf("encode Anthropic request: %w", err)
	}
	endpoint, err := providerProtocolEndpoint(b.cfg.APIBaseURL, "/messages")
	if err != nil {
		return apiCompletionResponse{}, err
	}
	resp, err := b.doProviderStreamRequest(ctx, endpoint, body, map[string]string{"anthropic-version": "2023-06-01"})
	if err != nil {
		return apiCompletionResponse{}, err
	}
	defer resp.Body.Close()
	if err := checkProviderHTTPResponse(resp, b.cfg.APIKey); err != nil {
		return apiCompletionResponse{}, err
	}

	var result apiCompletionResponse
	toolCalls := map[int]*apiToolCall{}
	toolOrder := []int{}
	done := false
	var eventName string
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if strings.TrimSpace(data) == "[DONE]" {
			done = true
			return nil
		}
		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("decode Anthropic stream event: %w", err)
		}
		typeName := strings.TrimSpace(event.Type)
		if typeName == "" {
			typeName = eventName
		}
		switch typeName {
		case "message_start":
			if event.Message.Usage != nil {
				result.Usage = tokenUsageFromAPI(*event.Message.Usage)
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				call := ensureAnthropicToolCall(toolCalls, &toolOrder, event.Index)
				call.ID = event.ContentBlock.ID
				call.Type = "function"
				call.Function.Name = event.ContentBlock.Name
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta", "text":
				content := sanitizeProviderOutput(event.Delta.Text, b.cfg.APIKey)
				if content != "" {
					result.Text += content
					trySend(msgCh, Message{Type: MessageText, Content: content})
				}
			case "thinking_delta", "signature_delta":
				reasoning := sanitizeProviderOutput(event.Delta.Thinking, b.cfg.APIKey)
				if reasoning != "" {
					trySend(msgCh, Message{Type: MessageThinking, Content: reasoning})
				}
			case "input_json_delta":
				call := ensureAnthropicToolCall(toolCalls, &toolOrder, event.Index)
				call.Function.Arguments += event.Delta.PartialJSON
			}
		case "message_delta":
			if event.Usage != nil {
				usage := tokenUsageFromAPI(*event.Usage)
				result.Usage.InputTokens = maxInt64(result.Usage.InputTokens, usage.InputTokens)
				result.Usage.OutputTokens = maxInt64(result.Usage.OutputTokens, usage.OutputTokens)
				result.Usage.CacheReadTokens = maxInt64(result.Usage.CacheReadTokens, usage.CacheReadTokens)
			}
		case "message_stop":
			done = true
		case "error":
			message := event.Error.Message
			if message == "" {
				message = "provider returned an Anthropic stream error"
			}
			return fmt.Errorf("Anthropic stream error: %s", sanitizeProviderOutput(message, b.cfg.APIKey))
		}
		return nil
	}
	if err := scanProviderSSE(resp.Body, func(name string, data []string) error {
		eventName = name
		dataLines = append(dataLines[:0], data...)
		return flush()
	}, func() bool { return done }); err != nil {
		return apiCompletionResponse{}, err
	}
	if !done {
		return apiCompletionResponse{}, errors.New("Anthropic stream ended before message_stop")
	}
	for _, index := range toolOrder {
		if call := toolCalls[index]; call != nil {
			result.ToolCalls = append(result.ToolCalls, *call)
		}
	}
	return result, nil
}

type anthropicStreamEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		Usage *apiUsage `json:"usage,omitempty"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Usage *apiUsage `json:"usage,omitempty"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func anthropicMessages(messages []apiMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		if message.Role == "system" {
			continue
		}
		role := message.Role
		if role == "tool" {
			role = "user"
			result = append(result, map[string]any{"role": role, "content": []any{map[string]any{"type": "tool_result", "tool_use_id": message.ToolCallID, "content": message.Content}}})
			continue
		}
		if len(message.ToolCalls) == 0 {
			result = append(result, map[string]any{"role": role, "content": message.Content})
			continue
		}
		blocks := make([]any, 0, len(message.ToolCalls)+1)
		if text, ok := message.Content.(string); ok && text != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": text})
		}
		for _, call := range message.ToolCalls {
			var input any = map[string]any{}
			if strings.TrimSpace(call.Function.Arguments) != "" {
				_ = json.Unmarshal([]byte(call.Function.Arguments), &input)
			}
			blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input})
		}
		result = append(result, map[string]any{"role": role, "content": blocks})
	}
	return result
}

func ensureAnthropicToolCall(calls map[int]*apiToolCall, order *[]int, index int) *apiToolCall {
	if call := calls[index]; call != nil {
		return call
	}
	call := &apiToolCall{Type: "function", ID: fmt.Sprintf("anthropic-call-%d", index)}
	calls[index] = call
	*order = append(*order, index)
	return call
}

func (b *openAICompatibleBackend) doProviderStreamRequest(ctx context.Context, endpoint string, body []byte, extraHeaders map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if b.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	resp, err := b.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	return resp, nil
}

func checkProviderHTTPResponse(resp *http.Response, secret string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, sanitizeProviderOutput(strings.TrimSpace(string(raw)), secret))
}

func providerProtocolEndpoint(raw, suffix string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", errors.New("API provider base URL is empty")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("API provider base URL must be an absolute HTTP(S) URL")
	}
	for _, known := range []string{"/chat/completions", "/responses", "/messages", "/models"} {
		if strings.HasSuffix(u.Path, known) {
			u.Path = strings.TrimSuffix(u.Path, known)
			break
		}
	}
	u.Path = strings.TrimRight(u.Path, "/") + suffix
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func scanProviderSSE(body io.Reader, handle func(eventName string, data []string) error, done func() bool) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var eventName string
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 {
			eventName = ""
			return nil
		}
		data := append([]string(nil), dataLines...)
		dataLines = nil
		name := eventName
		eventName = ""
		return handle(name, data)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			if done() {
				break
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read API stream: %w", err)
	}
	if len(dataLines) > 0 {
		if err := flush(); err != nil {
			return err
		}
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
