package localtoolpolicy

import "testing"

func TestProviderAdapterForRequiresCompleteBeforeCallCoverage(t *testing.T) {
	for _, provider := range []string{"claude", "codex", "cursor", "gemini", "pi", "opencode", "hermes", "kimi", "kiro"} {
		adapter, ok := ProviderAdapterFor(provider)
		if !ok || adapter.Provider != provider || adapter.HookEvent == "" {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, %v", provider, adapter, ok)
		}
	}
	for _, provider := range []string{"copilot", "new-local-cli"} {
		if adapter, ok := ProviderAdapterFor(provider); ok {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, want rejected", provider, adapter)
		}
	}
}

// The ACP family reaches the before-call seam through the daemon's own ACP
// client, so no settings-file writer and no harness may claim them.
func TestProviderAdapterACPFamilyIsClientGated(t *testing.T) {
	for _, provider := range []string{"hermes", "kimi", "kiro"} {
		adapter, ok := ProviderAdapterFor(provider)
		if !ok || !adapter.ACPClient || adapter.Harness {
			t.Fatalf("ProviderAdapterFor(%q) = %+v, want ACPClient without Harness", provider, adapter)
		}
	}
	for _, provider := range []string{"claude", "codex", "cursor", "gemini", "pi"} {
		if adapter, _ := ProviderAdapterFor(provider); adapter.ACPClient {
			t.Fatalf("ProviderAdapterFor(%q).ACPClient = true, want false", provider)
		}
	}
}

// Pi and OpenCode are the harness-wired providers: their before-call gate ships
// in a Firtal-owned extension the daemon installs into the task worktree, so no
// settings-file writer may claim them. Every other provider must NOT be marked
// as harness, or the daemon would skip writing the hook file it actually needs.
func TestProviderAdapterHarnessIsExtensionProvidersOnly(t *testing.T) {
	harnessProviders := map[string]bool{"pi": true, "opencode": true}
	for provider, adapter := range providerAdapters {
		if adapter.Harness != harnessProviders[provider] {
			t.Fatalf("ProviderAdapterFor(%q).Harness = %v, want %v", provider, adapter.Harness, harnessProviders[provider])
		}
	}
}
