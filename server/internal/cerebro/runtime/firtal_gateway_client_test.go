package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayClientCompleteSendsStructuredMessagesAndUsage(t *testing.T) {
	var got struct {
		Model    string           `json:"model"`
		Messages []GatewayMessage `json:"messages"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
