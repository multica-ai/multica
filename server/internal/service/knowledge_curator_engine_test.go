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

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestOpenAICompatibleCuratorEngineMissingConfigIsUnavailable(t *testing.T) {
	engine := NewOpenAICompatibleCuratorEngine(OpenAICompatibleCuratorConfig{})
	if _, err := engine.BuildEmbedding(context.Background(), "content"); !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable", err)
	}
}

func testIssue(title, description string) db.Issue {
	return db.Issue{
		Title:       title,
		Description: pgtype.Text{String: description, Valid: description != ""},
	}
}

func testCuratorConfig(baseURL, model string) OpenAICompatibleCuratorConfig {
	return OpenAICompatibleCuratorConfig{
		Chat: CuratorModelEndpointConfig{
			Provider: "test",
			BaseURL:  baseURL,
			Model:    model,
		},
		Timeout: time.Second,
	}
}

func testCuratorConfigWithEmbedding(baseURL, model, embeddingModel string) OpenAICompatibleCuratorConfig {
	cfg := testCuratorConfig(baseURL, model)
	cfg.Chat.APIKey = "key"
	cfg.Embedding = CuratorEmbeddingEndpointConfig{
		CuratorModelEndpointConfig: CuratorModelEndpointConfig{
			Provider: "test",
			BaseURL:  baseURL,
			APIKey:   "key",
			Model:    embeddingModel,
		},
		Dimensions: KnowledgeEmbeddingDimensions,
	}
	return cfg
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

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfigWithEmbedding(server.URL, "chat-model", "embedding-model"))
	if _, err := engine.BuildEmbedding(context.Background(), "content"); !errors.Is(err, ErrKnowledgeValidation) {
		t.Fatalf("BuildEmbedding error = %v, want ErrKnowledgeValidation", err)
	}
}

func TestOpenAICompatibleCuratorEngineGeneratesDraftWithoutEmbeddingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) < 2 || !strings.Contains(req.Messages[1].Content, "Output language: Chinese.") {
			t.Fatalf("draft prompt missing output language constraint: %#v", req.Messages)
		}
		if !strings.Contains(req.Messages[1].Content, "Use the output language for all human-readable JSON text fields") {
			t.Fatalf("draft prompt missing JSON field language instruction: %s", req.Messages[1].Content)
		}
		if !strings.Contains(req.Messages[1].Content, "Keep enum values such as type and confidence_status in English") {
			t.Fatalf("draft prompt missing enum language instruction: %s", req.Messages[1].Content)
		}
		if !strings.Contains(req.Messages[1].Content, "Preserve code, commands, error messages, API fields, file paths, identifiers, and proper nouns verbatim") {
			t.Fatalf("draft prompt missing technical text preservation instruction: %s", req.Messages[1].Content)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"title\":\"Draft\",\"type\":\"lesson\",\"domain_labels\":[\"ops\"],\"problem_pattern\":\"p\",\"trigger_conditions\":\"t\",\"diagnostic_steps\":\"d\",\"recommended_practice\":\"r\",\"anti_patterns\":\"a\",\"applicability\":\"app\",\"confidence_status\":\"medium\"}"}}]}`))
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfig(server.URL, "chat-model"))
	draft, err := engine.GenerateDraft(context.Background(), CuratorDraftInput{OutputLanguage: curatorOutputLanguageChinese})
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

func TestOpenAICompatibleCuratorEngineSummarizeSourceIncludesLanguageInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(req.Messages) < 2 || !strings.Contains(req.Messages[1].Content, "Output language: Chinese.") {
			t.Fatalf("summary prompt missing inferred language constraint: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"- 中文摘要"}}]}`))
	}))
	t.Cleanup(server.Close)

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfig(server.URL, "chat-model"))
	summary, err := engine.SummarizeSource(context.Background(), CuratorSourceBundle{
		Issue: testIssue("本地运行失败", "Error: context deadline exceeded"),
	})
	if err != nil {
		t.Fatalf("SummarizeSource error: %v", err)
	}
	if summary != "- 中文摘要" {
		t.Fatalf("summary = %q", summary)
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

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfig(server.URL, "chat-model"))
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

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfig(server.URL, "chat-model"))
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

	engine := NewOpenAICompatibleCuratorEngine(testCuratorConfig(server.URL, "missing-model"))
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
		Chat: CuratorModelEndpointConfig{
			Provider: "base-provider",
			BaseURL:  "https://base.example/v1",
			APIKey:   "base-key",
			Model:    "base-chat",
		},
		Embedding: CuratorEmbeddingEndpointConfig{
			CuratorModelEndpointConfig: CuratorModelEndpointConfig{
				Provider: "base-provider",
				BaseURL:  "https://base.example/v1",
				APIKey:   "base-key",
				Model:    "base-embedding",
			},
			Dimensions: KnowledgeEmbeddingDimensions,
		},
	}, settings)

	if got.Chat.Provider != "workspace-provider" || got.Chat.BaseURL != "https://provider.example/v1" || got.Chat.Model != "workspace-chat" || got.Embedding.Model != "workspace-embedding" {
		t.Fatalf("workspace settings were not applied: %#v", got)
	}
	if got.Chat.APIKey != "base-key" {
		t.Fatalf("Chat APIKey = %q, want base key preserved", got.Chat.APIKey)
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
		Chat: CuratorModelEndpointConfig{
			Provider: "base-provider",
			BaseURL:  "https://base.example/v1",
			APIKey:   "base-key",
			Model:    "base-chat",
		},
		Embedding: CuratorEmbeddingEndpointConfig{
			CuratorModelEndpointConfig: CuratorModelEndpointConfig{
				Provider: "base-provider",
				BaseURL:  "https://base.example/v1",
				APIKey:   "base-key",
				Model:    "base-embedding",
			},
			Dimensions: KnowledgeEmbeddingDimensions,
		},
	}, settings)
	if got.Chat.Provider != "" || got.Chat.Model != "" || got.Embedding.Model != "" {
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
	if got.ChatStatus.Provider != "custom" || got.ChatStatus.Model != "gpt-4.1-mini" || got.EmbeddingStatus.Model != "text-embedding-3-small" {
		t.Fatalf("unexpected recommendation: %#v", got)
	}
	if !got.ChatStatus.Supported || !got.EmbeddingStatus.Supported || got.ChatStatus.Error != "" || got.EmbeddingStatus.Error != "" {
		t.Fatalf("unexpected support flags/errors: %#v", got)
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
	if got.ChatStatus.Error != "" || !got.ChatStatus.Supported {
		t.Fatalf("chat status = %#v, want supported without error", got.ChatStatus)
	}
	if got.EmbeddingStatus.Supported {
		t.Fatalf("embedding supported = true, want false")
	}
	if got.EmbeddingStatus.Error == "" {
		t.Fatalf("expected embedding error")
	}
}

func TestProbeCuratorEndpointChatFailureDoesNotBlockEmbedding(t *testing.T) {
	chat := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "chat down", http.StatusBadGateway)
	}))
	t.Cleanup(chat.Close)
	embedding := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"text-embedding-3-small"}]}`))
		case "/embeddings":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"embedding": make([]float32, KnowledgeEmbeddingDimensions)}}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(embedding.Close)

	got, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
		ChatBaseURL:      chat.URL,
		EmbeddingBaseURL: embedding.URL,
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatalf("ProbeCuratorEndpoint error: %v", err)
	}
	if got.ChatStatus.Error == "" {
		t.Fatalf("expected chat error, got %#v", got.ChatStatus)
	}
	if !got.EmbeddingStatus.Supported || got.EmbeddingStatus.Error != "" {
		t.Fatalf("embedding status = %#v, want supported without error", got.EmbeddingStatus)
	}
}

func TestProbeCuratorEndpointAuthAndMalformedModelsFail(t *testing.T) {
	t.Run("auth", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		t.Cleanup(server.Close)

		got, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
			BaseURL: server.URL,
			Timeout: time.Second,
		})
		if err != nil {
			t.Fatalf("ProbeCuratorEndpoint error: %v", err)
		}
		if !strings.Contains(got.ChatStatus.Error, "/models returned 403") || !strings.Contains(got.EmbeddingStatus.Error, "/models returned 403") {
			t.Fatalf("status = %#v, want auth failures", got)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"models":["x"]}`))
		}))
		t.Cleanup(server.Close)

		got, err := ProbeCuratorEndpoint(context.Background(), CuratorEndpointProbeInput{
			BaseURL: server.URL,
			Timeout: time.Second,
		})
		if err != nil {
			t.Fatalf("ProbeCuratorEndpoint error: %v", err)
		}
		if !strings.Contains(got.ChatStatus.Error, "OpenAI-compatible") || !strings.Contains(got.EmbeddingStatus.Error, "OpenAI-compatible") {
			t.Fatalf("status = %#v, want malformed response failures", got)
		}
	})
}
