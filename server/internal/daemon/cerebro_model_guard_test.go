package daemon

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRunnableTaskModelKeepsModelInCatalog(t *testing.T) {
	got := runnableTaskModel(context.Background(), "claude", AgentEntry{}, Task{}, "claude-haiku-4-5-20251001", discardLog())
	if got != "claude-haiku-4-5-20251001" {
		t.Fatalf("model in the catalog was dropped: got %q", got)
	}
}

func TestRunnableTaskModelDropsModelOutsideCatalog(t *testing.T) {
	got := runnableTaskModel(context.Background(), "claude", AgentEntry{}, Task{}, "totally-not-a-real-model-xyz", discardLog())
	if got != "" {
		t.Fatalf("unrunnable model survived: got %q", got)
	}
}

func TestRunnableTaskModelFallsBackToAgentModel(t *testing.T) {
	task := Task{Agent: &AgentData{Model: "claude-opus-5"}}
	got := runnableTaskModel(context.Background(), "claude", AgentEntry{}, task, "gpt-5.4-mini", discardLog())
	if got != "claude-opus-5" {
		t.Fatalf("expected fallback to the agent's own model, got %q", got)
	}
}

func TestRunnableTaskModelDropsUnrunnableFallback(t *testing.T) {
	task := Task{Agent: &AgentData{Model: "also-not-real"}}
	got := runnableTaskModel(context.Background(), "claude", AgentEntry{}, task, "gpt-5.4-mini", discardLog())
	if got != "" {
		t.Fatalf("unrunnable fallback survived: got %q", got)
	}
}

// A provider with no discoverable catalog cannot prove the model wrong, so the
// pin has to survive — dropping it would break every legitimate override on a
// runtime we cannot enumerate.
func TestRunnableTaskModelKeepsModelWhenDiscoveryFails(t *testing.T) {
	got := runnableTaskModel(context.Background(), "no-such-provider", AgentEntry{}, Task{}, "some-model", discardLog())
	if got != "some-model" {
		t.Fatalf("pin dropped without proof: got %q", got)
	}
}

func TestRunnableTaskModelPassesEmptyModelThrough(t *testing.T) {
	got := runnableTaskModel(context.Background(), "claude", AgentEntry{}, Task{}, "", discardLog())
	if got != "" {
		t.Fatalf("empty model was rewritten: got %q", got)
	}
}
