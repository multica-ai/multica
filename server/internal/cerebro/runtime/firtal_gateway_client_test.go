package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayClientCompleteSendsStructuredMessagesAndUsage(t *testing.T) {
	var got struct {
		Model    string           `json:"model"`
		Messages []GatewayMessage `json:"messages"`
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/proxy/v1/chat/completions" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if gotAuth := r.Header.Get("Authorization"); gotAuth != "Bearer rk_test" {
			t.Fatalf("Authorization = %q", gotAuth)
		}
		if gotSession := r.Header.Get("X-Session-ID"); gotSession != "task-1" {
			t.Fatalf("X-Session-ID = %q", gotSession)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [{"message": {"content": "Hej fra server runtime"}}],
			"firtal": {"input_tokens": 12, "output_tokens": 5, "cached_input_tokens": 2, "cost_cents": 9}
		}`))
	}))
	defer srv.Close()

	client := NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL:   srv.URL,
		APIKey:    "rk_test",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
	}, srv.Client())

	completion, err := client.Complete(context.Background(), "", []GatewayMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "hello"},
	}, GatewayRequestMeta{TaskID: "task-1", AgentID: "agent-1", WorkspaceID: "workspace-1"})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completion.Output != "Hej fra server runtime" {
		t.Fatalf("Output = %q", completion.Output)
	}
	if completion.Usage.InputTokens != 12 || completion.Usage.OutputTokens != 5 || completion.Usage.CacheReadTokens != 2 || completion.Usage.CostCents != 9 {
		t.Fatalf("Usage = %+v", completion.Usage)
	}
	if got.Model != "claude-sonnet-4-6" {
		t.Fatalf("request model = %q", got.Model)
	}
	if len(got.Messages) != 2 || got.Messages[0].Role != "system" || got.Messages[1].Role != "user" {
		t.Fatalf("request messages = %+v", got.Messages)
	}
}

func TestGatewayClientCompleteWithToolsSendsToolsAndParsesToolCalls(t *testing.T) {
	var got struct {
		Tools    []GatewayToolDef `json:"tools"`
		Messages []GatewayMessage `json:"messages"`
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [{
				"message": {
					"content": null,
					"tool_calls": [
						{"id": "call_1", "type": "function", "function": {"name": "get_issue", "arguments": "{\"issue_id\":\"JEH-1\"}"}}
					]
				}
			}],
			"firtal": {"input_tokens": 100, "output_tokens": 8}
		}`))
	}))
	defer srv.Close()

	client := NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL:   srv.URL,
		APIKey:    "rk_test",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
	}, srv.Client())

	tools := []GatewayToolDef{{
		Type: "function",
		Function: GatewayToolFunction{
			Name:        "get_issue",
			Description: "fetch issue",
			Parameters:  map[string]any{"type": "object"},
		},
	}}
	completion, err := client.CompleteWithTools(context.Background(), "", []GatewayMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "Please summarise JEH-1"},
	}, tools, GatewayRequestMeta{TaskID: "task-1", AgentID: "agent-1", WorkspaceID: "workspace-1"})
	if err != nil {
		t.Fatalf("CompleteWithTools() error = %v", err)
	}
	if completion.Output != "" {
		t.Fatalf("Output = %q, want empty when only tool_calls are returned", completion.Output)
	}
	if len(completion.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v", completion.ToolCalls)
	}
	call := completion.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "get_issue" || !strings.Contains(call.Function.Arguments, "JEH-1") {
		t.Fatalf("tool call = %+v", call)
	}
	if len(got.Tools) != 1 || got.Tools[0].Function.Name != "get_issue" {
		t.Fatalf("request tools = %+v", got.Tools)
	}
}

func TestGatewayClientPassesToolMessageBack(t *testing.T) {
	var got struct {
		Messages []GatewayMessage `json:"messages"`
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices": [{"message": {"content": "ok"}}]}`))
	}))
	defer srv.Close()

	client := NewGatewayClient(FirtalGatewayRuntimeConfig{
		BaseURL:   srv.URL,
		APIKey:    "rk_test",
		Model:     "claude-sonnet-4-6",
		MaxTokens: 4096,
	}, srv.Client())

	if _, err := client.CompleteWithTools(context.Background(), "", []GatewayMessage{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "do it"},
		{Role: "assistant", ToolCalls: []GatewayToolCall{{ID: "c1", Type: "function", Function: GatewayToolCallFunction{Name: "get_issue", Arguments: "{}"}}}},
		{Role: "tool", ToolCallID: "c1", Content: `{"title":"x"}`},
	}, nil, GatewayRequestMeta{TaskID: "t1"}); err != nil {
		t.Fatalf("CompleteWithTools() error = %v", err)
	}
	if len(got.Messages) != 4 {
		t.Fatalf("messages len = %d, want 4: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[2].Role != "assistant" || len(got.Messages[2].ToolCalls) != 1 {
		t.Fatalf("assistant turn = %+v", got.Messages[2])
	}
	if got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "c1" || got.Messages[3].Content == "" {
		t.Fatalf("tool turn = %+v", got.Messages[3])
	}
}
