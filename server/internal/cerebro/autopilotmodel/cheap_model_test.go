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

// FIR-4492: the runtime's own cheap model is what makes TierCheap mean something
// on a provider the server has no catalog for. Before it, `--model cheap` on a
// hermes runtime resolved to "" and the wakeup ran on the agent's own model.
func TestResolveForRuntime(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		model        string
		runtimeCheap string
		want         string
		wantErr      bool
	}{
		{name: "uncatalogued provider uses the runtime's cheap model", provider: "hermes", model: TierCheap, runtimeCheap: "gemini-3.6-flash", want: "gemini-3.6-flash"},
		{name: "unset runtime cheap model keeps the old behaviour", provider: "hermes", model: TierCheap, runtimeCheap: "", want: ""},
		// The curated map is asserted against the provider's static catalog; a
		// hand-typed runtime value must not be able to override it.
		{name: "curated cheap model wins over the runtime value", provider: "claude", model: TierCheap, runtimeCheap: "gemini-3.6-flash", want: ModelHaiku},
		// A runtime cheap model does not make an uncatalogued provider strict:
		// its IDs still cannot be proven wrong here, so they pass through and the
		// daemon's live check is what catches them.
		{name: "unknown id on an uncatalogued provider still passes", provider: "hermes", model: "not-a-model-on-this-machine", runtimeCheap: "gemini-3.6-flash", want: "not-a-model-on-this-machine"},
		{name: "another provider's cheap model passes through", provider: "hermes", model: ModelHaiku, runtimeCheap: "gemini-3.6-flash", want: ModelHaiku},
		{name: "explicit id still passes through", provider: "hermes", model: "kimi-k3", runtimeCheap: "gemini-3.6-flash", want: "kimi-k3"},
		{name: "empty model stays empty", provider: "hermes", model: "", runtimeCheap: "gemini-3.6-flash", want: ""},
		// The runtime value does not loosen a catalogued provider's validation.
		{name: "cross-provider model still errors", provider: "codex", model: "claude-opus-5", runtimeCheap: "gpt-5.4-mini", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveForRuntime(tt.provider, tt.model, tt.runtimeCheap)
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
