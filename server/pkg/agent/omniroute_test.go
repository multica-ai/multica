package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveOmniRouteConfig(t *testing.T) {
	t.Parallel()
	if _, err := resolveOmniRouteConfig(map[string]string{}); err == nil {
		t.Fatal("expected missing base URL error")
	}
	if _, err := resolveOmniRouteConfig(map[string]string{omniRouteBaseURLKey: "http://router/v1"}); err == nil {
		t.Fatal("expected missing API key error")
	}
	cfg, err := resolveOmniRouteConfig(map[string]string{
		omniRouteBaseURLKey: "http://router/v1///",
		omniRouteAPIKeyKey:  "test-key",
	})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if cfg.BaseURL != "http://router/v1" || cfg.APIKey != "test-key" {
		t.Fatalf("config = %#v", cfg)
	}
}

func TestOmniRouteExecuteSendsAuthenticatedStreamingRequest(t *testing.T) {
	var gotRequest omniRouteChatRequest
	var gotAuth, gotSession string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("X-Session-Id")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotRequest); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("X-OmniRoute-Session-Id", "header-session")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"omni-1\",\"model\":\"auto\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	b := &omnirouteBackend{cfg: Config{Env: map[string]string{
		omniRouteBaseURLKey: server.URL + "/v1",
		omniRouteAPIKeyKey:  "secret-for-test",
	}, Logger: slog.Default()}}
	session, err := b.Execute(context.Background(), "do the thing", ExecOptions{
		Model:           "auto",
		SystemPrompt:    "You are an operator",
		ResumeSessionID: "prior-session",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "hello" || result.SessionID != "header-session" {
		t.Fatalf("result = %#v", result)
	}
	if gotAuth != "Bearer secret-for-test" || gotSession != "prior-session" {
		t.Fatalf("headers auth=%q session=%q", gotAuth, gotSession)
	}
	if gotRequest.Model != "auto" || !gotRequest.Stream || len(gotRequest.Messages) != 2 || gotRequest.Messages[0].Role != "system" {
		t.Fatalf("request = %#v", gotRequest)
	}
	if len(messages) != 3 || messages[0].Type != MessageStatus || messages[0].Status != "running" ||
		messages[1].Type != MessageText || messages[1].Content != "hello" ||
		messages[2].Type != MessageStatus || messages[2].Status != "completed" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestOmniRouteExecuteEmitsReasoningAndPreservesRequestedSessionWithoutHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"completion-id\",\"model\":\"auto\",\"choices\":[{\"delta\":{\"reasoning_content\":\"checking\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	b := &omnirouteBackend{cfg: Config{Env: map[string]string{
		omniRouteBaseURLKey: server.URL + "/v1",
		omniRouteAPIKeyKey:  "test-key",
	}}}
	session, err := b.Execute(context.Background(), "prompt", ExecOptions{
		Model:           "auto",
		ResumeSessionID: "requested-session",
		Timeout:         time.Second,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	for message := range session.Messages {
		messages = append(messages, message)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" || result.SessionID != "requested-session" {
		t.Fatalf("result = %#v", result)
	}
	if len(messages) != 4 || messages[1].Type != MessageThinking || messages[1].Content != "checking" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestConsumeOmniRouteSSEBuffersToolCallArguments(t *testing.T) {
	stream := strings.Join([]string{
		`data: {"id":"s1","model":"auto","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"q\":"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"roof\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"
	msgCh := make(chan Message, 8)
	var output strings.Builder
	result := Result{}
	var sessionID string
	if err := consumeOmniRouteSSE(strings.NewReader(stream), msgCh, &output, &result, &sessionID, "auto"); err != nil {
		t.Fatalf("consume: %v", err)
	}
	close(msgCh)
	var messages []Message
	for msg := range msgCh {
		messages = append(messages, msg)
	}
	if len(messages) != 1 || messages[0].Type != MessageToolUse || messages[0].Tool != "lookup" || messages[0].Input["q"] != "roof" {
		t.Fatalf("messages = %#v", messages)
	}
	if result.Usage["auto"].InputTokens != 12 || result.Usage["auto"].OutputTokens != 4 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func TestConsumeOmniRouteSSERejectsUnexpectedEOF(t *testing.T) {
	msgCh := make(chan Message, 2)
	var output strings.Builder
	result := Result{}
	var sessionID string
	err := consumeOmniRouteSSE(
		strings.NewReader(`data: {"choices":[{"delta":{"content":"partial"}}]}`+"\n\n"),
		msgCh,
		&output,
		&result,
		&sessionID,
		"auto",
	)
	if err == nil || !strings.Contains(err.Error(), "completion marker") {
		t.Fatalf("expected unexpected EOF error, got %v", err)
	}
}

func TestConsumeOmniRouteSSEDoesNotExposeToolCallOnUnexpectedEOF(t *testing.T) {
	msgCh := make(chan Message, 2)
	var output strings.Builder
	result := Result{}
	var sessionID string
	err := consumeOmniRouteSSE(
		strings.NewReader(`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"mutate","arguments":"{}"}}]}}]}`+"\n\n"),
		msgCh,
		&output,
		&result,
		&sessionID,
		"auto",
	)
	if err == nil || !strings.Contains(err.Error(), "completion marker") {
		t.Fatalf("expected unexpected EOF error, got %v", err)
	}
	select {
	case message := <-msgCh:
		t.Fatalf("unexpected message after truncated tool stream: %#v", message)
	default:
	}
}

func TestSanitizedHTTPErrorRedactsCredentialForms(t *testing.T) {
	message := sanitizedHTTPError(strings.NewReader(`{"error":"Authorization: Bearer bearer-secret", "x-api-key":"header-secret", "api_key":"json-secret"}`))
	for _, secret := range []string{"bearer-secret", "header-secret", "json-secret"} {
		if strings.Contains(message, secret) {
			t.Fatalf("sanitized error leaked %q: %s", secret, message)
		}
	}
}

func TestOmniRouteExecuteRejectsMissingCredentials(t *testing.T) {
	t.Parallel()
	for name, env := range map[string]string{
		"base": omniRouteBaseURLKey,
		"key":  omniRouteAPIKeyKey,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (&omnirouteBackend{cfg: Config{Env: map[string]string{
				env: "configured",
			}}}).Execute(context.Background(), "prompt", ExecOptions{})
			if err == nil || !strings.Contains(err.Error(), "OMNIROUTE_") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestOmniRouteExecuteRunsMCPToolLoop(t *testing.T) {
	var llmCalls int
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		w.Header().Set("Content-Type", "text/event-stream")
		if llmCalls == 1 {
			_, _ = io.WriteString(w, "data: {\"id\":\"turn-1\",\"model\":\"auto\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"mcp__crm__lookup\",\"arguments\":\"{\\\"id\\\":\\\"1\\\"}\"}}]}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		} else {
			_, _ = io.WriteString(w, "data: {\"id\":\"turn-2\",\"model\":\"auto\",\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer llm.Close()
	mcp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req omniRouteJSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "mcp-session")
		}
		result := map[string]interface{}{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{}
		case "tools/list":
			result = map[string]interface{}{"tools": []interface{}{map[string]interface{}{"name": "lookup", "inputSchema": map[string]interface{}{"type": "object"}}}}
		case "tools/call":
			result = map[string]interface{}{"content": []interface{}{map[string]interface{}{"type": "text", "text": "record"}}}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer mcp.Close()

	config := json.RawMessage(fmt.Sprintf(`{"mcpServers":{"crm":{"url":%q}}}`, mcp.URL))
	b := &omnirouteBackend{cfg: Config{Env: map[string]string{omniRouteBaseURLKey: llm.URL + "/v1", omniRouteAPIKeyKey: "test-key"}}}
	session, err := b.Execute(context.Background(), "lookup the record", ExecOptions{McpConfig: config, MaxTurns: 3, Timeout: time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var messages []Message
	for msg := range session.Messages {
		messages = append(messages, msg)
	}
	result := <-session.Result
	if result.Status != "completed" || result.Output != "done" || llmCalls != 2 {
		t.Fatalf("result=%#v calls=%d messages=%#v", result, llmCalls, messages)
	}
	if len(messages) != 5 || messages[1].Type != MessageToolUse || messages[2].Type != MessageToolResult || messages[3].Content != "done" ||
		messages[0].Type != MessageStatus || messages[0].Status != "running" || messages[4].Type != MessageStatus || messages[4].Status != "completed" {
		t.Fatalf("messages=%#v", messages)
	}
}
