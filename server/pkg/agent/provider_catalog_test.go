package agent

import (
	"strings"
	"testing"
)

func TestProviderCatalogContainsRequestedRuntimeFamilies(t *testing.T) {
	want := []string{
		"codex", "claude", "antigravity", "cursor", "grok", "opencode",
		"opencode-api", "opencode-zen", "opencode-go", "openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim",
	}
	for _, id := range want {
		desc, ok := ProviderByID(id)
		if !ok {
			t.Fatalf("ProviderByID(%q) not found", id)
		}
		if desc.DisplayName == "" || desc.LaunchHeader == "" {
			t.Fatalf("provider %q is missing user-facing metadata: %+v", id, desc)
		}
	}
}

func TestProviderCatalogDistinguishesCLIAndAPIProviders(t *testing.T) {
	for _, id := range []string{"codex", "claude", "antigravity", "cursor", "grok", "opencode"} {
		desc, _ := ProviderByID(id)
		if desc.Kind != ProviderKindCLI {
			t.Errorf("%s kind = %q, want CLI", id, desc.Kind)
		}
	}
	for _, id := range []string{"opencode-api", "opencode-zen", "opencode-go"} {
		desc, _ := ProviderByID(id)
		if desc.Kind != ProviderKindOpenCodeAPI {
			t.Errorf("%s kind = %q, want OpenCode API", id, desc.Kind)
		}
	}
	for _, id := range []string{"openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim"} {
		desc, _ := ProviderByID(id)
		if desc.Kind != ProviderKindOpenAICompatible {
			t.Errorf("%s kind = %q, want OpenAI-compatible", id, desc.Kind)
		}
	}
}

func TestResolveProviderAPIConfigUsesTaskEnvBeforeProcessEnv(t *testing.T) {
	desc, _ := ProviderByID("openrouter")
	env := map[string]string{
		desc.BaseURLEnv: "https://openrouter.ai/alternative/v1",
		desc.APIKeyEnv:  "task-key",
	}
	got, err := ResolveProviderAPIConfig("openrouter", env)
	if err != nil {
		t.Fatalf("ResolveProviderAPIConfig: %v", err)
	}
	if got.BaseURL != "https://openrouter.ai/alternative/v1" || got.APIKey != "task-key" {
		t.Fatalf("config = %+v, want task values", got)
	}
}

func TestProviderHostedCredentialsRejectUntrustedHostWithoutOperatorOverride(t *testing.T) {
	for _, provider := range []string{
		"opencode-api",
		"opencode-zen",
		"opencode-go",
		"openrouter",
		"vercel-ai-gateway",
		"nvidia-nim",
	} {
		t.Run(provider, func(t *testing.T) {
			desc, _ := ProviderByID(provider)
			env := map[string]string{
				desc.BaseURLEnv:                          "https://untrusted.example/v1",
				desc.APIKeyEnv:                           "fixture-key",
				"MULTICA_TRUSTED_PROVIDER_HOST_OVERRIDE": "1",
			}

			_, err := ResolveProviderAPIConfig(provider, env)
			if err == nil || !strings.Contains(err.Error(), "untrusted host") {
				t.Fatalf("ResolveProviderAPIConfig error = %v, want untrusted host rejection", err)
			}
		})
	}
}

func TestProviderHostedCredentialsAllowExplicitOperatorOverride(t *testing.T) {
	for _, provider := range []string{
		"opencode-api",
		"opencode-zen",
		"opencode-go",
		"openrouter",
		"vercel-ai-gateway",
		"nvidia-nim",
	} {
		t.Run(provider, func(t *testing.T) {
			desc, _ := ProviderByID(provider)
			env := map[string]string{
				desc.BaseURLEnv: "https://operator-approved.example/v1",
				desc.APIKeyEnv:  "fixture-key",
			}

			got, err := ResolveProviderAPIConfigWithTrustedHostOverride(provider, env, true)
			if err != nil {
				t.Fatalf("ResolveProviderAPIConfigWithTrustedHostOverride: %v", err)
			}
			if got.BaseURL != "https://operator-approved.example/v1" {
				t.Fatalf("trusted override base URL = %q, want configured endpoint", got.BaseURL)
			}
		})
	}
}

func TestResolveProviderAPIConfigAllowsKeylessLocalProviders(t *testing.T) {
	for _, id := range []string{"ollama", "lmstudio"} {
		cfg, err := ResolveProviderAPIConfig(id, nil)
		if err != nil {
			t.Fatalf("ResolveProviderAPIConfig(%q): %v", id, err)
		}
		if cfg.APIKey != "" {
			t.Errorf("%s API key = %q, want empty", id, cfg.APIKey)
		}
		if cfg.BaseURL == "" {
			t.Errorf("%s has no default base URL", id)
		}
	}
}

func TestAPIProviderTypesAreNotCustomCLIProfiles(t *testing.T) {
	for _, id := range []string{"opencode-api", "opencode-zen", "opencode-go", "openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim"} {
		if IsSupportedType(id) {
			t.Errorf("API provider %q must not be accepted as a CLI runtime profile family", id)
		}
		if !IsProviderType(id) {
			t.Errorf("API provider %q must be accepted by the provider factory", id)
		}
	}
}

func TestProviderCatalogCapabilitiesFailClosed(t *testing.T) {
	for _, id := range []string{"codex", "claude", "antigravity", "cursor", "grok", "opencode"} {
		if !ProviderSupportsCapability(id, ProviderCapabilityPrompt) || !ProviderSupportsCapability(id, ProviderCapabilityStreaming) {
			t.Errorf("CLI provider %q is missing baseline capabilities", id)
		}
	}
	if ProviderSupportsCapability("antigravity", ProviderCapabilityMCP) {
		t.Error("antigravity must not advertise MCP without a verified transport")
	}
	for _, id := range []string{"opencode-api", "opencode-zen", "opencode-go", "openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim"} {
		for _, capability := range []ProviderCapability{
			ProviderCapabilityPrompt,
			ProviderCapabilityStreaming,
			ProviderCapabilityCompletion,
			ProviderCapabilityCancellation,
			ProviderCapabilityModelDiscovery,
		} {
			if !ProviderSupportsCapability(id, capability) {
				t.Errorf("API provider %q is missing capability %q", id, capability)
			}
		}
		for _, capability := range []ProviderCapability{
			ProviderCapabilityUsage,
			ProviderCapabilityTools,
			ProviderCapabilityMCP,
		} {
			if ProviderSupportsCapability(id, capability) {
				t.Errorf("API provider %q advertises unproven capability %q", id, capability)
			}
		}
	}
	if ProviderSupportsCapability("unknown", ProviderCapabilityPrompt) {
		t.Error("unknown provider must fail closed")
	}
}

func TestProviderCatalogReturnsDeepCopies(t *testing.T) {
	first := ProviderCatalog()
	if len(first) == 0 || len(first[6].OptionalKeyEnv) == 0 || len(first[6].Capabilities) == 0 {
		t.Fatal("expected OpenCode API metadata in catalog")
	}
	first[6].OptionalKeyEnv[0] = "MUTATED"
	first[6].Capabilities[0] = "MUTATED"
	second, ok := ProviderByID("opencode-api")
	if !ok {
		t.Fatal("ProviderByID(opencode-api) not found")
	}
	if second.OptionalKeyEnv[0] == "MUTATED" || second.Capabilities[0] == "MUTATED" {
		t.Fatal("provider metadata was not copied defensively")
	}
}

func TestOpenCodeProviderEndpointsRemainDistinct(t *testing.T) {
	want := map[string]string{
		"opencode-api": "https://opencode.ai/inference/openai/v1",
		"opencode-zen": "https://opencode.ai/zen/v1",
		"opencode-go":  "https://opencode.ai/zen/go/v1",
	}
	for id, endpoint := range want {
		desc, ok := ProviderByID(id)
		if !ok {
			t.Fatalf("ProviderByID(%q) not found", id)
		}
		if desc.DefaultBaseURL != endpoint {
			t.Errorf("%s default endpoint = %q, want %q", id, desc.DefaultBaseURL, endpoint)
		}
		if desc.APIKeyEnv == "" || desc.BaseURLEnv == "" {
			t.Errorf("%s missing daemon-owned credential metadata: %+v", id, desc)
		}
	}
	if zen, _ := ProviderByID("opencode-zen"); zen.APIKeyEnv == "OPENCODE_API_KEY" {
		t.Error("OpenCode Zen must not reuse the Console API credential name")
	}
	if goProvider, _ := ProviderByID("opencode-go"); goProvider.APIKeyEnv == "OPENCODE_API_KEY" {
		t.Error("OpenCode Go must not reuse the Console API credential name")
	}
}

func TestOpenCodeModelProtocolsAreProviderSpecific(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		want     apiProtocol
	}{
		{provider: "opencode-zen", model: "gpt-5.6-sol", want: apiProtocolResponses},
		{provider: "opencode-zen", model: "claude-fable-5", want: apiProtocolAnthropicMessages},
		{provider: "opencode-zen", model: "deepseek-v4-pro", want: apiProtocolChatCompletions},
		{provider: "opencode-go", model: "gpt-5.6-luna", want: apiProtocolResponses},
		{provider: "opencode-go", model: "qwen3.7-max", want: apiProtocolAnthropicMessages},
		{provider: "opencode-go", model: "glm-5.2", want: apiProtocolChatCompletions},
	}
	for _, tc := range cases {
		got, ok := providerModelAPIProtocol(tc.provider, tc.model)
		if !ok || got != tc.want {
			t.Errorf("providerModelAPIProtocol(%q, %q) = %q, %v, want %q, true", tc.provider, tc.model, got, ok, tc.want)
		}
	}
	if _, ok := providerModelAPIProtocol("opencode-zen", "gemini-3.7-flash"); ok {
		t.Error("OpenCode Zen Gemini models must remain gated until a Google protocol adapter exists")
	}
	for _, provider := range []string{"opencode-zen", "opencode-go"} {
		if _, ok := providerModelAPIProtocol(provider, "unknown-model-family"); ok {
			t.Errorf("%s unknown model families must fail closed", provider)
		}
	}
}

func TestValidateAPIBaseURLRejectsUnsafeEndpoints(t *testing.T) {
	for _, raw := range []string{
		"file:///tmp/provider",
		"https://user:pass@example.test/v1",
		"https://example.test/v1?token=secret",
		"https://example.test/v1#fragment",
		"https://example.test/v1/../internal",
		"http://remote.example/v1",
		"https://example.test\\v1",
	} {
		if err := validateAPIBaseURL(raw, false); err == nil {
			t.Errorf("validateAPIBaseURL(%q) unexpectedly succeeded", raw)
		}
	}
	if err := validateAPIBaseURL("http://127.0.0.1:1234/v1", true); err != nil {
		t.Fatalf("loopback HTTP endpoint rejected: %v", err)
	}
	if err := validateAPIBaseURL("https://example.test/v1", false); err != nil {
		t.Fatalf("hosted HTTPS endpoint rejected: %v", err)
	}
}

func TestResolveProviderAPIProfileConfigUsesOnlyApprovedCredentialReference(t *testing.T) {
	env := map[string]string{"OPENROUTER_API_KEY": "profile-key"}
	got, err := ResolveProviderAPIProfileConfig(
		"openrouter", env, "https://openrouter.ai/profile/v1", "OPENROUTER_API_KEY",
	)
	if err != nil {
		t.Fatalf("ResolveProviderAPIProfileConfig: %v", err)
	}
	if got.BaseURL != "https://openrouter.ai/profile/v1" || got.APIKey != "profile-key" {
		t.Fatalf("config = %+v, want profile endpoint and local key", got)
	}
	if _, err := ResolveProviderAPIProfileConfig(
		"openrouter", env, "https://openrouter.ai/profile/v1", "EVIL_KEY",
	); err == nil {
		t.Fatal("unapproved credential environment unexpectedly accepted")
	}
}

func TestProviderProfileCredentialsRejectUntrustedHostWithoutOperatorOverride(t *testing.T) {
	env := map[string]string{
		"OPENROUTER_API_KEY":                     "fixture-key",
		"MULTICA_TRUSTED_PROVIDER_HOST_OVERRIDE": "1",
	}
	_, err := ResolveProviderAPIProfileConfig(
		"openrouter", env, "https://untrusted.example/v1", "OPENROUTER_API_KEY",
	)
	if err == nil || !strings.Contains(err.Error(), "untrusted host") {
		t.Fatalf("ResolveProviderAPIProfileConfig error = %v, want untrusted host rejection", err)
	}
}

func TestProviderProfileCredentialsAllowExplicitOperatorOverride(t *testing.T) {
	env := map[string]string{"OPENROUTER_API_KEY": "fixture-key"}
	got, err := ResolveProviderAPIProfileConfigWithTrustedHostOverride(
		"openrouter",
		env,
		"https://operator-approved.example/v1",
		"OPENROUTER_API_KEY",
		true,
	)
	if err != nil {
		t.Fatalf("ResolveProviderAPIProfileConfigWithTrustedHostOverride: %v", err)
	}
	if got.BaseURL != "https://operator-approved.example/v1" {
		t.Fatalf("trusted override base URL = %q, want configured endpoint", got.BaseURL)
	}
}

func TestProviderCredentialEnvAllowedIsProviderSpecific(t *testing.T) {
	if !ProviderCredentialEnvAllowed("opencode-zen", "OPENCODE_ZEN_API_KEY") {
		t.Fatal("OpenCode Zen credential environment should be allowed")
	}
	if ProviderCredentialEnvAllowed("opencode-zen", "OPENCODE_API_KEY") {
		t.Fatal("Console credential environment must not be accepted by Zen")
	}
	if ProviderCredentialEnvAllowed("codex", "CODEX_API_KEY") {
		t.Fatal("CLI provider must not accept API credential references")
	}
}
