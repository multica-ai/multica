package agent

// CEREBRO-PATCH(agent-firtal-gateway-tests): exercise managed gateway request, usage, cost, and model discovery.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFirtalGatewayExecuteCallsChatCompletions(t *testing.T) {
	t.Parallel()

	var seenPath, seenAuth, seenSessionID, seenUserID string
	var seenBody struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenSessionID = r.Header.Get("X-Session-ID")
		seenUserID = r.Header.Get("X-User-ID")
		if err := json.NewDecoder(r.Body).Decode(&seenBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"choices": [{"message": {"content": "Hej fra gateway"}}],
			"firtal": {"input_tokens": 10, "output_tokens": 4, "cached_input_tokens": 2, "cost_cents": 7}
		}`))
	}))
	t.Cleanup(srv.Close)

	backend := &firtalGatewayBackend{cfg: Config{Env: map[string]string{
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL": srv.URL,
		"FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY": "rk_test",
		"MULTICA_TASK_ID":                     "task-1",
		"MULTICA_AGENT_ID":                    "agent-1",
	}}}

	session, err := backend.Execute(context.Background(), "Sig hej", ExecOptions{
		Model:        "claude-sonnet-4-6",
		SystemPrompt: "Agent instructions",
		Timeout:      time.Second,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var streamed []string
	for msg := range session.Messages {
		streamed = append(streamed, msg.Content)
	}
	result := <-session.Result
	if result.Status != "completed" {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if result.Output != "Hej fra gateway" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(streamed) != 1 || streamed[0] != "Hej fra gateway" {
		t.Fatalf("streamed = %#v", streamed)
	}
	if seenPath != "/api/ai/proxy/v1/chat/completions" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenAuth != "Bearer rk_test" {
		t.Fatalf("Authorization = %q", seenAuth)
	}
	if seenSessionID != "task-1" || seenUserID != "agent-1" {
		t.Fatalf("context headers: session=%q user=%q", seenSessionID, seenUserID)
	}
	if seenBody.Model != "claude-sonnet-4-6" {
		t.Fatalf("model = %q", seenBody.Model)
	}
	if len(seenBody.Messages) != 2 {
		t.Fatalf("messages = %#v", seenBody.Messages)
	}
	if seenBody.Messages[0].Role != "system" || !strings.Contains(seenBody.Messages[0].Content, "Agent instructions") {
		t.Fatalf("system message = %#v", seenBody.Messages[0])
	}
	if seenBody.Messages[1].Role != "user" || seenBody.Messages[1].Content != "Sig hej" {
		t.Fatalf("user message = %#v", seenBody.Messages[1])
	}
	usage := result.Usage["claude-sonnet-4-6"]
	if usage.InputTokens != 10 || usage.OutputTokens != 4 || usage.CacheReadTokens != 2 || usage.CostCents != 7 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestFirtalGatewayExecuteRequiresConfig(t *testing.T) {
	t.Parallel()

	backend := &firtalGatewayBackend{cfg: Config{}}
	session, err := backend.Execute(context.Background(), "hej", ExecOptions{Timeout: time.Second})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for range session.Messages {
	}
	result := <-session.Result
	if result.Status != "failed" {
		t.Fatalf("status = %q", result.Status)
	}
	if !strings.Contains(result.Error, "FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL") {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestDiscoverFirtalGatewayModels(t *testing.T) {
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_KEY", "rk_test")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ai/proxy/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer rk_test" {
			t.Fatalf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "custom-model", "owned_by": "anthropic", "firtal_display_name": "Custom Model"}
			]
		}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("FIRTAL_DATA_REGISTRY_AI_GATEWAY_URL", srv.URL)

	models, err := discoverFirtalGatewayModels(context.Background())
	if err != nil {
		t.Fatalf("discoverFirtalGatewayModels: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v", models)
	}
	if models[0].ID != "custom-model" || models[0].Label != "Custom Model" || models[0].Provider != "anthropic" {
		t.Fatalf("model = %#v", models[0])
	}
	if !models[0].Default {
		t.Fatalf("expected first discovered model to become default when gateway default is absent")
	}
}
