package localtoolpolicy

import "testing"

func TestProviderAdapterForRequiresCompleteBeforeCallCoverage(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "cursor", "gemini"} {
		adapter, ok := ProviderAdapterFor(provider)
		if !ok || adapter.Provider != provider || adapter.HookEvent == "" {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, %v", provider, adapter, ok)
		}
	}
	for _, provider := range []string{"copilot", "opencode", "new-local-cli"} {
		if adapter, ok := ProviderAdapterFor(provider); ok {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, want rejected", provider, adapter)
		}
	}
}
