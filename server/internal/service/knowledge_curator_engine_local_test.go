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

func TestWorkspaceConfiguredCuratorEngine_BuildEmbedding_UsesBaseHTTPConfig(t *testing.T) {
	engine := &WorkspaceConfiguredCuratorEngine{
		base: OpenAICompatibleCuratorConfig{},
	}
	_, err := engine.BuildEmbedding(context.Background(), "content")
	if !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable", err)
	}
}

func TestWorkspaceConfiguredCuratorEngine_BuildEmbedding_IgnoresRemovedRuntimeMode(t *testing.T) {
	engine := &WorkspaceConfiguredCuratorEngine{
		base: OpenAICompatibleCuratorConfig{},
	}
	_, err := engine.BuildEmbedding(context.Background(), "content")
	if !errors.Is(err, ErrCuratorEngineUnavailable) {
		t.Fatalf("BuildEmbedding error = %v, want ErrCuratorEngineUnavailable", err)
	}
}
