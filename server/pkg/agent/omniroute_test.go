package agent

import (
	"context"
	"encoding/json"
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
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"omni-1\",\"model\":\"auto\",\"choices\":[{\"message\":{\"role\":\"assistant\",\"content\":\"hello\"}}]}\n\n")
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
	if result.Status != "completed" || result.Output != "hello" || result.SessionID != "omni-1" {
		t.Fatalf("result = %#v", result)
	}
	if gotAuth != "Bearer secret-for-test" || gotSession != "prior-session" {
		t.Fatalf("headers auth=%q session=%q", gotAuth, gotSession)
	}
	if gotRequest.Model != "auto" || !gotRequest.Stream || len(gotRequest.Messages) != 2 || gotRequest.Messages[0].Role != "system" {
		t.Fatalf("request = %#v", gotRequest)
	}
	if len(messages) != 1 || messages[0].Type != MessageText || messages[0].Content != "hello" {
		t.Fatalf("messages = %#v", messages)
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
