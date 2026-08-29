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
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const apiProviderMaxTurns = 20

const apiProviderMaxModelsBody = 1 << 20

type openAICompatibleBackend struct {
	cfg      Config
	provider ProviderDescriptor
}

func newOpenAICompatibleBackend(provider string, cfg Config) (Backend, error) {
	desc, ok := ProviderByID(provider)
	if !ok || (desc.Kind != ProviderKindOpenAICompatible && desc.Kind != ProviderKindOpenCodeAPI) {
		return nil, fmt.Errorf("provider %q is not an HTTP API provider", provider)
	}
	resolved, err := ResolveProviderAPIConfig(provider, cfg.Env)
	if cfg.APIBaseURL != "" {
		resolved.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	}
	if cfg.APIKey != "" {
		resolved.APIKey = strings.TrimSpace(cfg.APIKey)
	}
	if err != nil && cfg.APIBaseURL == "" && cfg.APIKey == "" {
		return nil, err
	}
	if resolved.BaseURL == "" {
		return nil, fmt.Errorf("provider %q has no API base URL", provider)
	}
	if err := validateAPIBaseURL(resolved.BaseURL, desc.LocalOnly); err != nil {
		return nil, fmt.Errorf("provider %q: %w", provider, err)
	}
	if desc.RequiresKey && resolved.APIKey == "" {
		return nil, fmt.Errorf("provider %q requires an API key", provider)
	}
	cfg.HTTPClient = safeProviderHTTPClient(cfg.HTTPClient)
	return &openAICompatibleBackend{cfg: cfg, provider: desc}, nil
}

// ListAPIModels retrieves an OpenAI-compatible provider catalog. The endpoint
// and credential are supplied by the daemon, and the returned catalog contains
// only provider metadata suitable for the model picker.
func ListAPIModels(ctx context.Context, provider string, cfg ProviderAPIConfig, defaultModel string, client *http.Client) (Catalog, error) {
	desc, ok := ProviderByID(provider)
	if !ok || (desc.Kind != ProviderKindOpenAICompatible && desc.Kind != ProviderKindOpenCodeAPI) {
		return Catalog{}, fmt.Errorf("provider %q is not an HTTP API provider", provider)
	}
	if err := validateAPIBaseURL(cfg.BaseURL, desc.LocalOnly); err != nil {
		return Catalog{}, fmt.Errorf("provider %q: %w", provider, err)
	}
	if desc.RequiresKey && strings.TrimSpace(cfg.APIKey) == "" {
		return Catalog{}, fmt.Errorf("provider %q requires an API key", provider)
	}
	client = safeProviderHTTPClient(client)
	endpoint, err := apiModelsEndpoint(cfg.BaseURL)
	if err != nil {
		return Catalog{}, fmt.Errorf("provider %q models endpoint: %w", provider, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Catalog{}, fmt.Errorf("provider %q models request: %w", provider, err)
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(cfg.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Catalog{}, fmt.Errorf("provider %q models request failed: %w", provider, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, apiProviderMaxModelsBody+1))
	if err != nil {
		return Catalog{}, fmt.Errorf("provider %q models response: %w", provider, err)
	}
	if len(body) > apiProviderMaxModelsBody {
		return Catalog{}, fmt.Errorf("provider %q models response exceeds %d bytes", provider, apiProviderMaxModelsBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Catalog{}, fmt.Errorf("provider %q models returned HTTP %d: %s", provider, resp.StatusCode, sanitizeProviderOutput(strings.TrimSpace(string(body)), cfg.APIKey))
	}
	var payload struct {
		Data   []apiModelEntry `json:"data"`
		Models []apiModelEntry `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Catalog{}, fmt.Errorf("provider %q models response is not JSON: %w", provider, err)
	}
	entries := payload.Data
	if len(entries) == 0 {
		entries = payload.Models
	}
	if len(entries) == 0 {
		return Catalog{}, fmt.Errorf("provider %q returned no models", provider)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	models := make([]Model, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, supported := providerModelAPIProtocol(provider, id); !supported {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		label := strings.TrimSpace(entry.Name)
		if label == "" {
			label = id
		}
		models = append(models, Model{ID: id, Label: label, Provider: provider, Default: id == strings.TrimSpace(defaultModel)})
	}
	if len(models) == 0 {
		return Catalog{}, fmt.Errorf("provider %q returned no usable models", provider)
	}
	return Catalog{Models: models}, nil
}

type apiModelEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (b *openAICompatibleBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, errors.New("API provider requires a non-empty prompt")
	}
	if opts.ResumeSessionID != "" {
		return nil, errors.New("API providers do not support session resumption; clear the prior session and retry")
	}
	model := strings.TrimSpace(opts.Model)
	if model == "" {
		model = strings.TrimSpace(b.cfg.DefaultModel)
	}
	if model == "" {
		return nil, fmt.Errorf("provider %q requires an explicit model", b.provider.ID)
	}

	servers, err := newAPIHTTPMCPServers(ctx, b.cfg.HTTPClient, opts.McpConfig)
	if err != nil {
		return nil, fmt.Errorf("%s MCP setup: %s", b.provider.ID, sanitizeProviderOutput(err.Error(), b.cfg.APIKey))
	}
	tools := flattenAPIHTTPMCPTools(servers)
	maxTurns := opts.MaxTurns
	if maxTurns <= 0 || maxTurns > apiProviderMaxTurns {
		maxTurns = apiProviderMaxTurns
	}

	runCtx, cancel := runContext(ctx, opts.Timeout)
	msgCh := make(chan Message, 256)
	resCh := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(msgCh)
		defer close(resCh)

		messages := []apiMessage{}
		if strings.TrimSpace(opts.SystemPrompt) != "" {
			messages = append(messages, apiMessage{Role: "system", Content: opts.SystemPrompt})
		}
		messages = append(messages, apiMessage{Role: "user", Content: prompt})
		var output strings.Builder
		var usage TokenUsage
		status := "completed"
		var finalErr string
		startedAt := time.Now()
		trySend(msgCh, Message{Type: MessageStatus, Status: "running"})

		for turn := 0; turn < maxTurns; turn++ {
			response, err := b.doStreamingRequest(runCtx, model, messages, tools, msgCh)
			if err != nil {
				status = statusForAPIContext(runCtx)
				if status == "completed" {
					status = "failed"
				}
				finalErr = sanitizeProviderOutput(err.Error(), b.cfg.APIKey)
				break
			}
			usage = mergeTokenUsage(usage, response.Usage)
			output.WriteString(response.Text)
			if len(response.ToolCalls) == 0 {
				break
			}
			assistant := apiMessage{Role: "assistant", Content: response.Text, ToolCalls: response.ToolCalls}
			messages = append(messages, assistant)
			for _, call := range response.ToolCalls {
				serverTool, ok := tools[call.Function.Name]
				if !ok {
					status = "failed"
					finalErr = sanitizeProviderOutput(fmt.Sprintf("model requested unknown MCP tool %q", call.Function.Name), b.cfg.APIKey)
					break
				}
				args := map[string]any{}
				if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
					if err := json.Unmarshal([]byte(raw), &args); err != nil {
						status = "failed"
						finalErr = sanitizeProviderOutput(fmt.Sprintf("MCP tool %q returned invalid arguments: %v", call.Function.Name, err), b.cfg.APIKey)
						break
					}
				}
				trySend(msgCh, Message{Type: MessageToolUse, Tool: call.Function.Name, CallID: call.ID, Input: args})
				toolOutput, toolErr := serverTool.server.call(runCtx, serverTool.Name, args)
				if toolErr != nil {
					toolOutput = "MCP tool error: " + sanitizeProviderOutput(toolErr.Error(), b.cfg.APIKey)
				}
				toolOutput = sanitizeProviderOutput(toolOutput, b.cfg.APIKey)
				trySend(msgCh, Message{Type: MessageToolResult, Tool: call.Function.Name, CallID: call.ID, Output: toolOutput})
				messages = append(messages, apiMessage{Role: "tool", ToolCallID: call.ID, Content: toolOutput})
			}
			if finalErr != "" {
				break
			}
			if turn == maxTurns-1 {
				status = "failed"
				finalErr = fmt.Sprintf("API provider exceeded the %d-turn MCP tool limit", maxTurns)
			}
		}
		if runCtx.Err() != nil && finalErr == "" {
			status = statusForAPIContext(runCtx)
			finalErr = runCtx.Err().Error()
		}
		if finalErr != "" {
			finalErr = sanitizeProviderOutput(finalErr, b.cfg.APIKey)
		}
		usageMap := map[string]TokenUsage{}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 || usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0 {
			usageMap[model] = usage
		}
		resCh <- Result{Status: status, Output: output.String(), Error: finalErr, DurationMs: time.Since(startedAt).Milliseconds(), Usage: usageMap}
	}()
	return &Session{Messages: msgCh, Result: resCh}, nil
}

func statusForAPIContext(ctx context.Context) string {
	switch ctx.Err() {
	case context.DeadlineExceeded:
		return "timeout"
	case context.Canceled:
		return "aborted"
	default:
		return "completed"
	}
}

// sanitizeProviderOutput applies the shared diagnostic sanitizer and then
// removes the exact daemon-owned credential. Provider responses are untrusted:
// a model or upstream error can echo an Authorization value even though the
// key was never placed in the request body. This protects both streamed
// messages and the final task result.
func sanitizeProviderOutput(value, secret string) string {
	value = sanitizeAgentDiagnostic(value)
	if secret = strings.TrimSpace(secret); secret != "" {
		value = strings.ReplaceAll(value, secret, "[REDACTED]")
	}
	return value
}

type apiMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []apiToolCall `json:"tool_calls,omitempty"`
}

type apiToolCall struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Function apiFunction `json:"function"`
}

type apiFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type apiToolDefinition struct {
	Type     string          `json:"type"`
	Function apiToolFunction `json:"function"`
}

type apiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type apiCompletionResponse struct {
	Text      string
	ToolCalls []apiToolCall
	Usage     TokenUsage
}

type apiUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	PromptCached     int64 `json:"prompt_cached_tokens"`
	CacheRead        int64 `json:"cache_read_tokens"`
}

type apiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *apiUsage       `json:"usage,omitempty"`
	Error *apiStreamError `json:"error,omitempty"`
}

type apiStreamError struct {
	Message string `json:"message"`
}

func safeProviderHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	// Never allow a provider client to follow a redirect with an Authorization
	// header. The daemon owns the endpoint, but an upstream or test transport
	// can still return a cross-origin redirect.
	clone := *client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func (b *openAICompatibleBackend) doStreamingRequest(ctx context.Context, model string, messages []apiMessage, tools map[string]*apiHTTPMCPTool, msgCh chan<- Message) (apiCompletionResponse, error) {
	protocol, ok := providerModelAPIProtocol(b.provider.ID, model)
	if !ok {
		return apiCompletionResponse{}, fmt.Errorf("provider %q does not support model %q", b.provider.ID, model)
	}
	switch protocol {
	case apiProtocolResponses:
		return b.doResponsesStreamingRequest(ctx, model, messages, tools, msgCh)
	case apiProtocolAnthropicMessages:
		return b.doAnthropicStreamingRequest(ctx, model, messages, tools, msgCh)
	default:
		return b.doChatCompletionsStreamingRequest(ctx, model, messages, tools, msgCh)
	}
}

func (b *openAICompatibleBackend) doChatCompletionsStreamingRequest(ctx context.Context, model string, messages []apiMessage, tools map[string]*apiHTTPMCPTool, msgCh chan<- Message) (apiCompletionResponse, error) {
	payload := map[string]any{"model": model, "messages": messages, "stream": true, "stream_options": map[string]any{"include_usage": true}}
	if opts := strings.TrimSpace(b.cfg.Env["MULTICA_API_REASONING_EFFORT"]); opts != "" {
		payload["reasoning_effort"] = opts
	}
	if len(tools) > 0 {
		definitions := make([]apiToolDefinition, 0, len(tools))
		for name, tool := range tools {
			definitions = append(definitions, apiToolDefinition{Type: "function", Function: apiToolFunction{Name: name, Description: tool.Description, Parameters: tool.InputSchema}})
		}
		payload["tools"] = definitions
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return apiCompletionResponse{}, fmt.Errorf("encode API request: %w", err)
	}
	endpoint, err := completionEndpoint(b.cfg.APIBaseURL)
	if err != nil {
		return apiCompletionResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return apiCompletionResponse{}, fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if b.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)
	}
	resp, err := b.cfg.HTTPClient.Do(req)
	if err != nil {
		return apiCompletionResponse{}, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return apiCompletionResponse{}, fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, sanitizeProviderOutput(strings.TrimSpace(string(raw)), b.cfg.APIKey))
	}

	var result apiCompletionResponse
	toolCalls := map[int]*apiToolCall{}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var dataLines []string
	sawDone := false
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = nil
		if strings.TrimSpace(data) == "[DONE]" {
			sawDone = true
			return nil
		}
		var chunk apiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decode API stream event: %w", err)
		}
		if chunk.Error != nil {
			message := strings.TrimSpace(chunk.Error.Message)
			if message == "" {
				message = "provider returned an API stream error"
			}
			return fmt.Errorf("API stream error: %s", sanitizeProviderOutput(message, b.cfg.APIKey))
		}
		if chunk.Usage != nil {
			result.Usage = tokenUsageFromAPI(*chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				content := sanitizeProviderOutput(choice.Delta.Content, b.cfg.APIKey)
				result.Text += content
				trySend(msgCh, Message{Type: MessageText, Content: content})
			}
			reasoning := choice.Delta.Reasoning
			if reasoning == "" {
				reasoning = choice.Delta.ReasoningContent
			}
			if reasoning != "" {
				reasoning = sanitizeProviderOutput(reasoning, b.cfg.APIKey)
				trySend(msgCh, Message{Type: MessageThinking, Content: reasoning})
			}
			for _, call := range choice.Delta.ToolCalls {
				current := toolCalls[call.Index]
				if current == nil {
					current = &apiToolCall{ID: call.ID, Type: call.Type}
					toolCalls[call.Index] = current
				}
				if current.ID == "" {
					current.ID = call.ID
				}
				if current.Type == "" {
					current.Type = call.Type
				}
				current.Function.Name += call.Function.Name
				current.Function.Arguments += call.Function.Arguments
			}
		}
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return apiCompletionResponse{}, err
			}
			if sawDone {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return apiCompletionResponse{}, fmt.Errorf("read API stream: %w", err)
	}
	if len(dataLines) > 0 {
		if err := flush(); err != nil {
			return apiCompletionResponse{}, err
		}
	}
	if !sawDone {
		return apiCompletionResponse{}, errors.New("API stream ended before the terminal [DONE] marker")
	}
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	for _, index := range indices {
		result.ToolCalls = append(result.ToolCalls, *toolCalls[index])
	}
	return result, nil
}

func tokenUsageFromAPI(value apiUsage) TokenUsage {
	input := value.InputTokens
	if input == 0 {
		input = value.PromptTokens
	}
	output := value.OutputTokens
	if output == 0 {
		output = value.CompletionTokens
	}
	cacheRead := value.CacheRead
	if cacheRead == 0 {
		cacheRead = value.PromptCached
	}
	return TokenUsage{InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead}
}

func mergeTokenUsage(a, b TokenUsage) TokenUsage {
	a.InputTokens += b.InputTokens
	a.OutputTokens += b.OutputTokens
	a.CacheReadTokens += b.CacheReadTokens
	a.CacheWriteTokens += b.CacheWriteTokens
	a.CostUSDTicks += b.CostUSDTicks
	return a
}

func completionEndpoint(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", errors.New("API provider base URL is empty")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("API provider base URL must be an absolute HTTP(S) URL")
	}
	if strings.HasSuffix(u.Path, "/chat/completions") {
		return u.String(), nil
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	return u.String(), nil
}

func apiModelsEndpoint(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", errors.New("API provider base URL is empty")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("API provider base URL must be an absolute HTTP(S) URL")
	}
	if strings.HasSuffix(u.Path, "/models") {
		return u.String(), nil
	}
	if strings.HasSuffix(u.Path, "/chat/completions") {
		u.Path = strings.TrimSuffix(u.Path, "/chat/completions")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/models"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

type apiHTTPMCPTool struct {
	server      *apiHTTPMCPServer
	Name        string
	Description string
	InputSchema json.RawMessage
}

type apiHTTPMCPServer struct {
	Name    string
	URL     string
	Client  *http.Client
	Headers map[string]string
	Tools   []apiHTTPMCPTool
	nextID  atomic.Int64
}

type apiMCPConfig struct {
	MCPServers map[string]apiMCPServerConfig `json:"mcpServers"`
	MCP        map[string]apiMCPServerConfig `json:"mcp"`
}

type apiMCPServerConfig struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func newAPIHTTPMCPServers(ctx context.Context, client *http.Client, raw json.RawMessage) (map[string]*apiHTTPMCPTool, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return map[string]*apiHTTPMCPTool{}, nil
	}
	var cfg apiMCPConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("invalid MCP config: %w", err)
	}
	configs := cfg.MCPServers
	if len(configs) == 0 {
		configs = cfg.MCP
	}
	tools := map[string]*apiHTTPMCPTool{}
	for name, serverCfg := range configs {
		if serverCfg.Type != "" && serverCfg.Type != "http" && serverCfg.Type != "streamable-http" && serverCfg.Type != "sse" {
			return nil, fmt.Errorf("MCP server %q uses unsupported transport %q; API providers currently require HTTP MCP", name, serverCfg.Type)
		}
		if strings.TrimSpace(serverCfg.URL) == "" {
			return nil, fmt.Errorf("MCP server %q has no URL", name)
		}
		mcpURL, err := validateAPIHTTPMCPURL(serverCfg.URL)
		if err != nil {
			return nil, fmt.Errorf("MCP server %q URL: %w", name, err)
		}
		server := &apiHTTPMCPServer{Name: name, URL: mcpURL, Client: client, Headers: serverCfg.Headers}
		if err := server.initialize(ctx, serverCfg.Headers); err != nil {
			return nil, fmt.Errorf("MCP server %q initialize: %w", name, err)
		}
		for _, tool := range server.Tools {
			callName := apiMCPCallName(name, tool.Name)
			if _, exists := tools[callName]; exists {
				return nil, fmt.Errorf("MCP tool name collision after qualification: %q", callName)
			}
			tools[callName] = &apiHTTPMCPTool{server: server, Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema}
		}
	}
	return tools, nil
}

func validateAPIHTTPMCPURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "\\") {
		return "", errors.New("URL must not contain backslashes")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("URL must be an absolute HTTP(S) URL")
	}
	if u.User != nil {
		return "", errors.New("URL must not contain credentials")
	}
	return u.String(), nil
}

func flattenAPIHTTPMCPTools(tools map[string]*apiHTTPMCPTool) map[string]*apiHTTPMCPTool {
	return tools
}

func apiMCPCallName(server, tool string) string {
	clean := func(value string) string {
		var out strings.Builder
		for _, r := range value {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
				out.WriteRune(r)
			} else {
				out.WriteByte('_')
			}
		}
		return out.String()
	}
	return clean(server) + "__" + clean(tool)
}

func (s *apiHTTPMCPServer) initialize(ctx context.Context, headers map[string]string) error {
	s.nextID.Store(1)
	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
		Error *apiRPCError `json:"error"`
	}
	if err := s.rpc(ctx, headers, map[string]any{"jsonrpc": "2.0", "id": s.nextID.Add(1), "method": "initialize", "params": map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "multica-api-provider", "version": "1"}}}, &response); err != nil {
		return err
	}
	if response.Error != nil {
		return errors.New(response.Error.Message)
	}
	if response.Result.ProtocolVersion == "" {
		return errors.New("MCP initialize returned no protocol version")
	}
	if err := s.rpc(ctx, headers, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}}, nil); err != nil {
		return err
	}
	var listed struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				Description string          `json:"description"`
				InputSchema json.RawMessage `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
		Error *apiRPCError `json:"error"`
	}
	if err := s.rpc(ctx, headers, map[string]any{"jsonrpc": "2.0", "id": s.nextID.Add(1), "method": "tools/list", "params": map[string]any{}}, &listed); err != nil {
		return err
	}
	if listed.Error != nil {
		return errors.New(listed.Error.Message)
	}
	for _, tool := range listed.Result.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return errors.New("MCP tools/list returned a tool with no name")
		}
		s.Tools = append(s.Tools, apiHTTPMCPTool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema})
	}
	return nil
}

type apiRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *apiHTTPMCPServer) call(ctx context.Context, name string, args map[string]any) (string, error) {
	var response struct {
		Result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *apiRPCError `json:"error"`
	}
	if err := s.rpc(ctx, s.Headers, map[string]any{"jsonrpc": "2.0", "id": s.nextID.Add(1), "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, &response); err != nil {
		return "", err
	}
	if response.Error != nil {
		return "", errors.New(response.Error.Message)
	}
	var out strings.Builder
	for _, block := range response.Result.Content {
		if block.Type == "text" {
			out.WriteString(block.Text)
		} else if block.Type != "" {
			out.WriteString("[")
			out.WriteString(block.Type)
			out.WriteString(" content]")
		}
	}
	if response.Result.IsError {
		return out.String(), errors.New(out.String())
	}
	return out.String(), nil
}

func (s *apiHTTPMCPServer) rpc(ctx context.Context, headers map[string]string, payload map[string]any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<10))
		return fmt.Errorf("MCP returned HTTP %d: %s", resp.StatusCode, sanitizeAgentDiagnostic(strings.TrimSpace(string(raw))))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode MCP response: %w", err)
	}
	return nil
}
