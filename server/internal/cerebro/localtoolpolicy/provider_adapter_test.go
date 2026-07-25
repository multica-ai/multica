package localtoolpolicy

import "testing"

func TestProviderAdapterForRequiresCompleteBeforeCallCoverage(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "cursor", "gemini", "pi"} {
		adapter, ok := ProviderAdapterFor(provider)
		if !ok || adapter.Provider != provider || adapter.HookEvent == "" {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, %v", provider, adapter, ok)
		}
	}
	for _, provider := range []string{"copilot", "opencode", "hermes", "new-local-cli"} {
		if adapter, ok := ProviderAdapterFor(provider); ok {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, want rejected", provider, adapter)
		}
	}
}

// Pi is the only harness-wired provider: its before-call gate ships in the
// Firtal Pi harness extension, so no settings-file writer may claim it.
func TestProviderAdapterHarnessIsPiOnly(t *testing.T) {
	for provider, adapter := range providerAdapters {
		if adapter.Harness != (provider == "pi") {
			t.Fatalf("ProviderAdapterFor(%q).Harness = %v, want %v", provider, adapter.Harness, provider == "pi")
		}
	}
}
