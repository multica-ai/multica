package service

import (
	"context"
	"errors"
	"testing"
)

func TestLocalCuratorEngine_SummarizeSource_ReturnsError(t *testing.T) {
	engine := &LocalCuratorEngine{}
	_, err := engine.SummarizeSource(context.Background(), CuratorSourceBundle{})
	if !errors.Is(err, ErrCuratorLocalSummarizeUnavailable) {
		t.Fatalf("SummarizeSource error = %v, want ErrCuratorLocalSummarizeUnavailable", err)
	}
}

func TestLocalCuratorEngine_BuildEmbedding_ReturnsError(t *testing.T) {
	engine := &LocalCuratorEngine{}
	_, err := engine.BuildEmbedding(context.Background(), "content")
	if !errors.Is(err, ErrCuratorLocalEmbeddingUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorLocalEmbeddingUnavailable", err)
	}
}

func TestWorkspaceConfiguredCuratorEngine_BuildEmbedding_LocalMode(t *testing.T) {
	engine := &WorkspaceConfiguredCuratorEngine{
		base: OpenAICompatibleCuratorConfig{RuntimeMode: "local"},
	}
	_, err := engine.BuildEmbedding(context.Background(), "content")
	if !errors.Is(err, ErrCuratorLocalEmbeddingUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorLocalEmbeddingUnavailable", err)
	}
}

func TestWorkspaceConfiguredCuratorEngine_BuildEmbedding_CloudMode_UsesBase(t *testing.T) {
	// When not in local mode, should delegate to base engine.
	// With empty config the engine is a MissingCuratorEngine which returns ErrCuratorEngineUnavailable.
	engine := &WorkspaceConfiguredCuratorEngine{
		base: OpenAICompatibleCuratorConfig{RuntimeMode: "cloud"},
	}
	_, err := engine.BuildEmbedding(context.Background(), "content")
	if !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable (from MissingCuratorEngine)", err)
	}
}
