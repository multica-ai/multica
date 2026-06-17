package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
		RuntimeMode:    "cloud",
	}, settings)

	if got.Provider != "workspace-provider" || got.BaseURL != "https://provider.example/v1" || got.Model != "workspace-chat" || got.EmbeddingModel != "workspace-embedding" || got.RuntimeMode != "external" {
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
