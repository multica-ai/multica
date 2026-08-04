package autopilotmodel

import (
	"errors"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// A cheap model the target provider does not accept is the whole bug (FIR-4492),
// so every curated value has to be in that provider's catalog. This test fails
// if a provider is renamed or a model ID is dropped from the catalog.
func TestCheapModelsAreInProviderCatalog(t *testing.T) {
	for provider, model := range cheapModels {
		ids, ok := agent.StaticCatalogIDs(provider)
		if !ok {
			t.Errorf("provider %q has no authoritative catalog, so %q cannot be vouched for", provider, model)
			continue
		}
		if !agent.StaticCatalogSupports(provider, model) {
			t.Errorf("cheap model %q is not in %q's catalog (has: %v)", model, provider, ids)
		}
	}
}

func TestResolveForProvider(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		want     string
		wantErr  bool
	}{
		{name: "empty stays empty", provider: "claude", model: "", want: ""},
		{name: "cheap on claude", provider: "claude", model: TierCheap, want: ModelHaiku},
		{name: "cheap on codex", provider: "codex", model: TierCheap, want: "gpt-5.4-mini"},
		// No curated cheap model must never become a guessed ID — an uncatalogued
		// provider would fail the run on `--model cheap`.
		{name: "cheap on uncatalogued provider", provider: "opencode", model: TierCheap, want: ""},
		{name: "supported id passes through", provider: "claude", model: "claude-opus-5", want: "claude-opus-5"},
		// The reported failure: 28 woken runs died on this exact pair.
		{name: "claude cheap model on codex substitutes", provider: "codex", model: ModelHaiku, want: "gpt-5.4-mini"},
		{name: "codex cheap model on claude substitutes", provider: "claude", model: "gpt-5.4-mini", want: ModelHaiku},
		// An uncatalogued provider's IDs cannot be proven wrong, so they pass.
		{name: "uncatalogued provider passes anything", provider: "opencode", model: "opencode-go:kimi-k3", want: "opencode-go:kimi-k3"},
		{name: "specific cross-provider model errors", provider: "codex", model: "claude-opus-5", wantErr: true},
		{name: "unknown model errors", provider: "claude", model: "claude-opus-99", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveForProvider(tt.provider, tt.model)
			if tt.wantErr {
				if !errors.Is(err, ErrModelNotOnProvider) {
					t.Fatalf("want ErrModelNotOnProvider, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
