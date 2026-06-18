package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOpenAICompatibleCuratorEngineMissingConfigIsUnavailable(t *testing.T) {
	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{})
	if _, err := engine.BuildEmbedding(context.Background(), "content"); !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable", err)
	}
}

func TestOpenAICompatibleCuratorEngineRejectsWrongEmbeddingDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{
		Provider:       "test",
		BaseURL:        server.URL,
		APIKey:         "key",
		Model:          "chat-model",
		EmbeddingModel: "embedding-model",
		Timeout:        time.Second,
	})
	if _, err := engine.BuildEmbedding(context.Background(), "content"); !errors.Is(err, ErrKnowledgeValidation) {
		t.Fatalf("BuildEmbedding error = %v, want ErrKnowledgeValidation", err)
	}
}

func TestOpenAICompatibleCuratorEngineGeneratesDraftWithoutEmbeddingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"Draft\",\"type\":\"lesson\",\"domain_labels\":[\"ops\"],\"problem_pattern\":\"p\",\"trigger_conditions\":\"t\",\"diagnostic_steps\":\"d\",\"recommended_practice\":\"r\",\"anti_patterns\":\"a\",\"applicability\":\"app\",\"confidence_status\":\"medium\"}"}}]}`))
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{
		Provider: "test",
		BaseURL:  server.URL,
		Model:    "chat-model",
		Timeout:  time.Second,
	})
	draft, err := engine.GenerateDraft(context.Background(), CuratorDraftInput{})
	if err != nil {
		t.Fatalf("GenerateDraft error: %v", err)
	}
	if draft.Title != "Draft" || draft.Type != "lesson" {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	if _, err := engine.BuildEmbedding(context.Background(), "content"); !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable", err)
	}
}

func TestOpenAICompatibleCuratorEngineExtractsDraftJSONFromText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		content := "Here is the draft:\n```json\n{\"title\":\"Draft\",\"type\":\"playbook\",\"domain_labels\":[\"ops\"],\"problem_pattern\":\"p { brace }\",\"trigger_conditions\":\"t\",\"diagnostic_steps\":\"d\",\"recommended_practice\":\"r\",\"anti_patterns\":\"a\",\"applicability\":\"app\",\"confidence_status\":\"high\"}\n```\nThis is ready for review."
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{
		Provider: "test",
		BaseURL:  server.URL,
		Model:    "chat-model",
		Timeout:  time.Second,
	})
	draft, err := engine.GenerateDraft(context.Background(), CuratorDraftInput{})
	if err != nil {
		t.Fatalf("GenerateDraft error: %v", err)
	}
	if draft.Title != "Draft" || draft.Type != "playbook" || draft.ConfidenceStatus != "high" {
		t.Fatalf("unexpected draft: %#v", draft)
	}
}

func TestOpenAICompatibleCuratorEngineNormalizesDraftArrayTextFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		content := `{
			"title":"Draft",
			"type":"lesson",
			"domain_labels":["ops"],
			"problem_pattern":["first signal","second signal"],
			"trigger_conditions":["when sync fails","when queue retries"],
			"diagnostic_steps":["check logs","inspect queue"],
			"recommended_practice":["persist retry payload","drain on shutdown"],
			"anti_patterns":["drop transient failures"],
			"applicability":["local runtime message sync"],
			"confidence_status":"medium"
		}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": content}}},
		})
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{
		Provider: "test",
		BaseURL:  server.URL,
		Model:    "chat-model",
		Timeout:  time.Second,
	})
	draft, err := engine.GenerateDraft(context.Background(), CuratorDraftInput{})
	if err != nil {
		t.Fatalf("GenerateDraft error: %v", err)
	}
	if draft.TriggerConditions != "- when sync fails\n- when queue retries" {
		t.Fatalf("TriggerConditions = %q", draft.TriggerConditions)
	}
	if draft.DiagnosticSteps != "- check logs\n- inspect queue" {
		t.Fatalf("DiagnosticSteps = %q", draft.DiagnosticSteps)
	}
	if draft.RecommendedPractice != "- persist retry payload\n- drain on shutdown" {
		t.Fatalf("RecommendedPractice = %q", draft.RecommendedPractice)
	}
}

func TestOpenAICompatibleCuratorEngineWrapsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not found", http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{
		Provider: "test",
		BaseURL:  server.URL,
		Model:    "missing-model",
		Timeout:  time.Second,
	})
	_, err := engine.GenerateDraft(context.Background(), CuratorDraftInput{})
	if !errors.Is(err, ErrCuratorProvider) {
		t.Fatalf("GenerateDraft error = %v, want ErrCuratorProvider", err)
	}
}

func TestApplyWorkspaceCuratorSettingsOverridesBaseConfig(t *testing.T) {
	settings, err := json.Marshal(map[string]any{
		"knowledge_curator": map[string]any{
			"enabled":         true,
			"provider":        "workspace-provider",
			"base_url":        " https://provider.example/v1/ ",
			"model":           "workspace-chat",
			"embedding_model": "workspace-embedding",
			"runtime_mode":    "external",
		},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	got := applyWorkspaceCuratorSettings(OpenAICompatibleCuratorConfig{
		Provider:       "base-provider",
		BaseURL:        "https://base.example/v1",
		APIKey:         "base-key",
		Model:          "base-chat",
		EmbeddingModel: "base-embedding",
	}, settings)

	if got.Provider != "workspace-provider" || got.BaseURL != "https://provider.example/v1" || got.Model != "workspace-chat" || got.EmbeddingModel != "workspace-embedding" {
		t.Fatalf("workspace settings were not applied: %#v", got)
	}
	if got.APIKey != "base-key" {
		t.Fatalf("APIKey = %q, want base key preserved", got.APIKey)
	}
}

func TestApplyWorkspaceCuratorSettingsDisabledReturnsUnavailableConfig(t *testing.T) {
	settings, err := json.Marshal(map[string]any{
		"knowledge_curator": map[string]any{"enabled": false},
	})
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	got := applyWorkspaceCuratorSettings(OpenAICompatibleCuratorConfig{
		Provider:       "base-provider",
		BaseURL:        "https://base.example/v1",
		APIKey:         "base-key",
		Model:          "base-chat",
		EmbeddingModel: "base-embedding",
	}, settings)
	if got.Provider != "" || got.Model != "" || got.EmbeddingModel != "" {
		t.Fatalf("disabled workspace config should clear provider/model fields: %#v", got)
	}
}

func TestProbeCuratorEndpointOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4.1-mini"},{"id":"text-embedding-3-small"}]}`))
		case "/embeddings":
			var req struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode embeddings request: %v", err)
			}
			if req.Model != "text-embedding-3-small" {
				t.Fatalf("embedding model = %q", req.Model)
			}
			w.Header().Set("Content-Type", "application/json")
			embedding := make([]float32, KnowledgeEmbeddingDimensions)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": embedding}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	got, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("ProbeCuratorEndpoint error: %v", err)
	}
	if got.Provider != "custom" || got.Model != "gpt-4.1-mini" || got.EmbeddingModel != "text-embedding-3-small" {
		t.Fatalf("unexpected recommendation: %#v", got)
	}
	if !got.ChatSupported || !got.EmbeddingSupported || len(got.Warnings) != 0 {
		t.Fatalf("unexpected support flags/warnings: %#v", got)
	}
}

func TestProbeCuratorEndpointEmbeddingWarning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"llama3.1"},{"id":"nomic-embed-text"}]}`))
		case "/embeddings":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	got, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
		BaseURL: server.URL,
		Timeout: time.Second,
	})
	if err != nil {
		t.Fatalf("ProbeCuratorEndpoint error: %v", err)
	}
	if got.EmbeddingSupported {
		t.Fatalf("EmbeddingSupported = true, want false")
	}
	if len(got.Warnings) == 0 {
		t.Fatalf("expected embedding warning")
	}
}

func TestProbeCuratorEndpointAuthAndMalformedModelsFail(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		t.Cleanup(server.Close)

		_, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
			BaseURL: server.URL,
			Timeout: time.Second,
		})
		if err == nil || !strings.Contains(err.Error(), "/models returned 403") {
			t.Fatalf("error = %v, want auth failure", err)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"models":["x"]}`))
		}))
		t.Cleanup(server.Close)

		_, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
			BaseURL: server.URL,
			Timeout: time.Second,
		})
		if err == nil || !strings.Contains(err.Error(), "OpenAI-compatible") {
			t.Fatalf("error = %v, want malformed response failure", err)
		}
	})
}
