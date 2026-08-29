package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAICompatibleExecuteStreamsReasoningTextAndUsage(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization = %q, want bearer token", got)
		}
		var payload struct {
			Model    string       `json:"model"`
			Messages []apiMessage `json:"messages"`
			Stream   bool         `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.Model != "fixture-model" || !payload.Stream || len(payload.Messages) != 1 {
			t.Errorf("request payload = %#v", payload)
		}
		writeSSE(w,
			`{"choices":[{"delta":{"reasoning_content":"checking"}}]}`,
			`{"choices":[{"delta":{"content":"hello"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5}}`,
			"[DONE]",
		)
	}))
	defer func() {
		server.CloseClientConnections()
		server.Close()
	}()

	backend, err := New("ollama", Config{
		APIBaseURL:   server.URL + "/v1",
		APIKey:       "test-key",
		DefaultModel: "fixture-model",
	})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "hello" || result.Error != "" {
		t.Fatalf("result = %#v", result)
	}
	if result.Usage["fixture-model"].InputTokens != 3 || result.Usage["fixture-model"].OutputTokens != 5 {
		t.Fatalf("usage = %#v", result.Usage)
	}
	if requests.Load() != 1 {
		t.Fatalf("request count = %d, want 1", requests.Load())
	}
	if !hasMessage(messages, MessageThinking, "checking") || !hasMessage(messages, MessageText, "hello") {
		t.Fatalf("streamed messages = %#v", messages)
	}
}

func TestListAPIModelsNormalizesCatalogAndMarksDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("models request = %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		writeJSON(w, map[string]any{"data": []any{
			map[string]any{"id": "z-model", "name": "Z Model"},
			map[string]any{"id": "a-model"},
			map[string]any{"id": "a-model", "name": "duplicate"},
			map[string]any{"id": ""},
		}})
	}))
	defer server.Close()

	catalog, err := ListAPIModels(context.Background(), "openrouter", ProviderAPIConfig{BaseURL: server.URL + "/v1", APIKey: "test-key"}, "z-model", nil)
	if err != nil {
		t.Fatalf("ListAPIModels: %v", err)
	}
	if len(catalog.Models) != 2 || catalog.Models[0].ID != "a-model" || catalog.Models[1].ID != "z-model" {
		t.Fatalf("models = %#v", catalog.Models)
	}
	if catalog.Models[1].Label != "Z Model" || !catalog.Models[1].Default {
		t.Fatalf("default model = %#v", catalog.Models[1])
	}
}

func TestListAPIModelsFiltersUnsupportedOpenCodeProtocolModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %s, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"gemini-3.7-flash"},{"id":"gpt-5.6-sol"},{"id":"deepseek-v4-pro"}]}`)
	}))
	defer server.Close()

	catalog, err := ListAPIModels(context.Background(), "opencode-zen", ProviderAPIConfig{BaseURL: server.URL + "/v1", APIKey: "test-key"}, "gpt-5.6-sol", nil)
	if err != nil {
		t.Fatalf("ListAPIModels: %v", err)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("models = %+v, want only supported protocol models", catalog.Models)
	}
	for _, model := range catalog.Models {
		if model.ID == "gemini-3.7-flash" {
			t.Fatal("unsupported Gemini model was advertised")
		}
	}
}

func TestOpenCodeZenRoutesResponsesModelsToResponsesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("request path = %q, want /v1/responses", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer zen-key" {
			t.Errorf("authorization = %q, want Zen bearer token", r.Header.Get("Authorization"))
		}
		var payload struct {
			Model  string           `json:"model"`
			Input  []map[string]any `json:"input"`
			Stream bool             `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode Responses request: %v", err)
		}
		if payload.Model != "gpt-5.6-sol" || len(payload.Input) != 1 || !payload.Stream {
			t.Errorf("Responses payload = %#v", payload)
		}
		writeNamedSSE(w,
			"response.output_text.delta", `{"type":"response.output_text.delta","delta":"hello"}`,
			"response.completed", `{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":5}}}`,
		)
	}))
	defer server.Close()

	backend, err := New("opencode-zen", Config{APIBaseURL: server.URL + "/v1", APIKey: "zen-key", DefaultModel: "gpt-5.6-sol"})
	if err != nil {
		t.Fatalf("New(opencode-zen): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "hello" || result.Usage["gpt-5.6-sol"].OutputTokens != 5 {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenCodeGoRoutesAnthropicModelsToMessagesAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("request path = %q, want /v1/messages", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer go-key" || r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("headers = authorization %q, anthropic-version %q", r.Header.Get("Authorization"), r.Header.Get("anthropic-version"))
		}
		var payload struct {
			Model    string           `json:"model"`
			System   string           `json:"system"`
			Messages []map[string]any `json:"messages"`
			Stream   bool             `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode Anthropic request: %v", err)
		}
		if payload.Model != "qwen3.7-max" || payload.System != "runtime" || len(payload.Messages) != 1 || !payload.Stream {
			t.Errorf("Anthropic payload = %#v", payload)
		}
		writeNamedSSE(w,
			"message_start", `{"type":"message_start","message":{"usage":{"input_tokens":4}}}`,
			"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
			"message_delta", `{"type":"message_delta","usage":{"output_tokens":6}}`,
			"message_stop", `{"type":"message_stop"}`,
		)
	}))
	defer server.Close()

	backend, err := New("opencode-go", Config{APIBaseURL: server.URL + "/v1", APIKey: "go-key", DefaultModel: "qwen3.7-max"})
	if err != nil {
		t.Fatalf("New(opencode-go): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say done", ExecOptions{SystemPrompt: "runtime"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" || result.Usage["qwen3.7-max"].InputTokens != 4 || result.Usage["qwen3.7-max"].OutputTokens != 6 {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAICompatibleExecuteFailsClosedWithoutDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w, `{"choices":[{"delta":{"content":"partial"}}]}`)
	}))
	defer server.Close()

	backend, err := New("ollama", Config{APIBaseURL: server.URL + "/v1", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "terminal [DONE]") {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAICompatibleExecuteRedactsAPIErrorSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"api_key=fixture-secret"}}`))
	}))
	defer server.Close()

	backend, err := New("openrouter", Config{APIBaseURL: server.URL + "/v1", APIKey: "fixture-secret", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(openrouter): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || strings.Contains(result.Error, "fixture-secret") || !strings.Contains(result.Error, "REDACTED") {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAICompatibleExecuteRedactsAPISecretsFromStreamedOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w,
			`{"choices":[{"delta":{"content":"the key is fixture-secret"}}]}`,
			`{"choices":[{"delta":{"reasoning_content":"fixture-secret"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	backend, err := New("openrouter", Config{APIBaseURL: server.URL + "/v1", APIKey: "fixture-secret", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(openrouter): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for message := range session.Messages {
		if strings.Contains(message.Content, "fixture-secret") || strings.Contains(message.Output, "fixture-secret") {
			t.Fatalf("streamed message leaked provider secret: %#v", message)
		}
	}
	result := <-session.Result
	if strings.Contains(result.Output, "fixture-secret") || strings.Contains(result.Error, "fixture-secret") {
		t.Fatalf("result leaked provider secret: %#v", result)
	}
	if !strings.Contains(result.Output, "REDACTED") {
		t.Fatalf("result output = %q, want redaction marker", result.Output)
	}
}

func TestOpenAICompatibleExecuteFailsClosedOnProviderStreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(w,
			`{"error":{"message":"provider key fixture-secret was rejected"}}`,
			"[DONE]",
		)
	}))
	defer server.Close()

	backend, err := New("openrouter", Config{APIBaseURL: server.URL + "/v1", APIKey: "fixture-secret", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(openrouter): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || strings.Contains(result.Error, "fixture-secret") || !strings.Contains(result.Error, "REDACTED") {
		t.Fatalf("result = %#v", result)
	}
}

func TestOpenAICompatibleProviderClientRefusesRedirects(t *testing.T) {
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		writeSSE(w, `{"choices":[{"delta":{"content":"unexpected"}}]}`, "[DONE]")
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	backend, err := New("openrouter", Config{
		APIBaseURL:   redirect.URL + "/v1",
		APIKey:       "fixture-secret",
		DefaultModel: "fixture-model",
		HTTPClient:   &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error { return nil }},
	})
	if err != nil {
		t.Fatalf("New(openrouter): %v", err)
	}
	session, err := backend.Execute(context.Background(), "say hello", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" || !strings.Contains(result.Error, "HTTP 307") {
		t.Fatalf("result = %#v", result)
	}
	if targetRequests.Load() != 0 {
		t.Fatal("provider client followed a redirect to the target origin")
	}
}

func TestOpenAICompatibleMCPRejectsNonHTTPURL(t *testing.T) {
	backend, err := New("ollama", Config{APIBaseURL: "http://127.0.0.1:1/v1", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
	}
	config := json.RawMessage(`{"mcpServers":{"unsafe":{"type":"http","url":"file:///tmp/mcp"}}}`)
	if _, err := backend.Execute(context.Background(), "say hello", ExecOptions{McpConfig: config}); err == nil || !strings.Contains(err.Error(), "absolute HTTP(S)") {
		t.Fatalf("Execute error = %v, want non-HTTP MCP URL rejection", err)
	}
}

func TestOpenAICompatibleExecuteRunsHTTPMCPTool(t *testing.T) {
	var apiRequests atomic.Int64
	var mcpCalls atomic.Int64
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Fixture") != "yes" {
			t.Errorf("MCP fixture header missing on %s", r.URL.Path)
		}
		var request struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode MCP request: %v", err)
		}
		switch request.Method {
		case "initialize":
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]any{"protocolVersion": "2025-03-26"}})
		case "notifications/initialized":
			writeJSON(w, map[string]any{})
		case "tools/list":
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 3, "result": map[string]any{"tools": []any{map[string]any{
				"name": "lookup", "description": "look up a record", "inputSchema": map[string]any{"type": "object"},
			}}}})
		case "tools/call":
			mcpCalls.Add(1)
			if request.Params["name"] != "lookup" {
				t.Errorf("MCP tool name = %#v, want lookup", request.Params["name"])
			}
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 4, "result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "tool-ok"}}}})
		default:
			t.Errorf("unexpected MCP method %q", request.Method)
			writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": 0, "error": map[string]any{"message": "unexpected method"}})
		}
	}))
	defer mcp.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := apiRequests.Add(1)
		var payload struct {
			Messages []struct {
				Role       string        `json:"role"`
				ToolCalls  []apiToolCall `json:"tool_calls"`
				ToolCallID string        `json:"tool_call_id"`
				Content    string        `json:"content"`
			} `json:"messages"`
			Tools []apiToolDefinition `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode API request: %v", err)
		}
		if requestNumber == 1 {
			if len(payload.Tools) != 1 || payload.Tools[0].Function.Name != "inventory__lookup" {
				t.Errorf("API tools = %#v", payload.Tools)
			}
			writeSSE(w,
				`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"inventory__lookup","arguments":"{\"id\":\"42\"}"}}]}}]}`,
				`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
				"[DONE]",
			)
			return
		}
		if len(payload.Messages) != 3 || payload.Messages[2].Role != "tool" || payload.Messages[2].ToolCallID != "call-1" || payload.Messages[2].Content != "tool-ok" {
			t.Errorf("follow-up messages = %#v", payload.Messages)
		}
		writeSSE(w, `{"choices":[{"delta":{"content":"finished"}}]}`, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, "[DONE]")
	}))
	defer api.Close()

	backend, err := New("openrouter", Config{APIBaseURL: api.URL + "/v1", APIKey: "test-key", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(openrouter): %v", err)
	}
	mcpConfig := json.RawMessage(fmt.Sprintf(`{"mcpServers":{"inventory":{"type":"http","url":%q,"headers":{"X-Fixture":"yes"}}}}`, mcp.URL))
	session, err := backend.Execute(context.Background(), "look up record", ExecOptions{McpConfig: mcpConfig})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "finished" || result.Error != "" {
		t.Fatalf("result = %#v", result)
	}
	if apiRequests.Load() != 2 || mcpCalls.Load() != 1 {
		t.Fatalf("API requests = %d, MCP calls = %d", apiRequests.Load(), mcpCalls.Load())
	}
}

func TestOpenAICompatibleConstructorRequiresHostedKeyButAllowsLocalKeyless(t *testing.T) {
	if _, err := New("openrouter", Config{APIBaseURL: "http://127.0.0.1:1/v1"}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("New(openrouter) error = %v, want missing key error", err)
	}
	if _, err := New("ollama", Config{APIBaseURL: "http://127.0.0.1:1/v1", DefaultModel: "fixture-model"}); err != nil {
		t.Fatalf("New(ollama) keyless error = %v", err)
	}
	if _, err := New("ollama", Config{APIBaseURL: "file:///tmp/ollama", DefaultModel: "fixture-model"}); err == nil || !strings.Contains(err.Error(), "HTTP(S)") {
		t.Fatalf("New(ollama) invalid URL error = %v", err)
	}
}

func TestOpenAICompatibleExecuteHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(250 * time.Millisecond)
		writeSSE(w, `{"choices":[{"delta":{"content":"too late"}}]}`, "[DONE]")
	}))
	defer server.Close()

	backend, err := New("ollama", Config{APIBaseURL: server.URL + "/v1", DefaultModel: "fixture-model"})
	if err != nil {
		t.Fatalf("New(ollama): %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	session, err := backend.Execute(ctx, "wait", ExecOptions{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "timeout" || result.Error == "" {
		t.Fatalf("result = %#v", result)
	}
}

func writeSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range events {
		fmt.Fprintf(w, "data: %s\n\n", event)
	}
}

func writeNamedSSE(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for index := 0; index+1 < len(events); index += 2 {
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", events[index], events[index+1])
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func hasMessage(messages []Message, typ MessageType, content string) bool {
	for _, message := range messages {
		if message.Type == typ && message.Content == content {
			return true
		}
	}
	return false
}
