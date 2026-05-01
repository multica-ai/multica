package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/pkg/llm"
)

func TestOpenAICompatClient_Complete(t *testing.T) {
	responseBody := map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": `{"result":"ok"}`,
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responseBody)
	}))
	defer srv.Close()

	client := llm.NewOpenAICompatClient(llm.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Model:   "test-model",
	})

	resp, err := client.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != `{"result":"ok"}` {
		t.Errorf("unexpected content: %q", resp.Content)
	}
}

func TestOpenAICompatClient_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := llm.NewOpenAICompatClient(llm.Config{
		BaseURL: srv.URL,
		APIKey:  "test-key",
	})

	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenAICompatClient_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer srv.Close()

	client := llm.NewOpenAICompatClient(llm.Config{
		BaseURL: srv.URL,
		APIKey:  "key",
	})
	_, err := client.Complete(context.Background(), llm.CompletionRequest{
		Messages: []llm.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
