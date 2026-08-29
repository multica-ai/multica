package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestProbeAPIProvidersDiscoversConfiguredProvidersWithoutExecutables(t *testing.T) {
	setMissingCLIs(t)

	for _, desc := range agent.ProviderCatalog() {
		if desc.Kind != agent.ProviderKindOpenAICompatible && desc.Kind != agent.ProviderKindOpenCodeAPI {
			continue
		}
		scheme := "https"
		if desc.LocalOnly {
			t.Setenv(desc.BaseURLEnv, "http://127.0.0.1:12345/v1")
		} else {
			t.Setenv(desc.BaseURLEnv, scheme+"://"+desc.ID+".example.test/v1")
		}
		if desc.RequiresKey {
			t.Setenv(desc.APIKeyEnv, "key-for-"+desc.ID)
		}
	}

	oldProbe := probeAPIProviderEndpoint
	var probed []string
	var probedMu sync.Mutex
	probeAPIProviderEndpoint = func(_ context.Context, provider string, cfg agent.ProviderAPIConfig) error {
		probedMu.Lock()
		defer probedMu.Unlock()
		probed = append(probed, provider+"="+cfg.BaseURL+"="+cfg.APIKey)
		return nil
	}
	t.Cleanup(func() { probeAPIProviderEndpoint = oldProbe })

	got := probeAgentCLIs()
	for _, provider := range []string{"opencode-api", "opencode-zen", "opencode-go", "openrouter", "vercel-ai-gateway", "ollama", "lmstudio", "nvidia-nim"} {
		entry, ok := got[provider]
		if !ok {
			t.Fatalf("provider %q was not discovered: %v", provider, got)
		}
		if entry.Path != "" {
			t.Errorf("provider %q has executable path %q, want none", provider, entry.Path)
		}
		if entry.APIBaseURL == "" {
			t.Errorf("provider %q has no API base URL", provider)
		}
	}
	if len(probed) != 8 {
		t.Fatalf("probed %d API providers, want 8: %v", len(probed), probed)
	}
}

func TestProbeAPIProvidersSkipsUnconfiguredOrInvalidProviders(t *testing.T) {
	setMissingCLIs(t)
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENROUTER_BASE_URL", "https://openrouter.example.test/v1")
	t.Setenv("OLLAMA_BASE_URL", "file:///tmp/ollama")
	t.Setenv("LMSTUDIO_BASE_URL", "file:///tmp/lmstudio")
	t.Setenv("OPENCODE_API_KEY", "")

	oldProbe := probeAPIProviderEndpoint
	probed := 0
	probeAPIProviderEndpoint = func(_ context.Context, _ string, _ agent.ProviderAPIConfig) error {
		probed++
		return nil
	}
	t.Cleanup(func() { probeAPIProviderEndpoint = oldProbe })

	got := probeAgentCLIs()
	if _, ok := got["openrouter"]; ok {
		t.Fatal("openrouter was discovered without its required API key")
	}
	if _, ok := got["ollama"]; ok {
		t.Fatal("ollama was discovered with an invalid file URL")
	}
	if probed != 0 {
		t.Fatalf("invalid or unconfigured providers were probed %d times", probed)
	}
}

func TestAPIProviderProbeRefusesRedirectsBeforeForwardingCredential(t *testing.T) {
	var targetRequests atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/v1/models", http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()

	err := defaultProbeAPIProviderEndpoint(context.Background(), "openrouter", agent.ProviderAPIConfig{
		BaseURL: redirect.URL + "/v1",
		APIKey:  "probe-secret",
	})
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("probe error = %v, want redirect refusal", err)
	}
	if targetRequests.Load() != 0 {
		t.Fatal("API provider probe followed redirect to another origin")
	}
}

func TestDetectBuiltinRuntimesRegistersAPIProviderWithoutVersionProbe(t *testing.T) {
	oldProbe := probeAPIProviderEndpoint
	probeAPIProviderEndpoint = func(_ context.Context, _ string, _ agent.ProviderAPIConfig) error {
		return nil
	}
	t.Cleanup(func() { probeAPIProviderEndpoint = oldProbe })

	d := &Daemon{
		cfg: Config{Agents: map[string]AgentEntry{
			"openrouter": {APIBaseURL: "https://openrouter.example.test/v1", apiKey: "test-key"},
		}},
		logger:        quietLogger(),
		agentVersions: make(map[string]string),
		skippedAgents: make(map[string]string),
	}

	got, _, _ := d.detectBuiltinRuntimes(context.Background())
	want := []map[string]string{{
		"name":                  "OpenRouter",
		"type":                  "openrouter",
		"version":               "",
		"status":                "online",
		"provider_capabilities": "prompt,streaming,completion,cancellation,model-discovery,usage,tools,mcp",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("detected runtimes = %#v, want %#v", got, want)
	}
}

func TestRuntimeProfilesRegisterAPIProvidersWithoutExecutables(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "profile-test-key")
	oldProbe := probeAPIProviderEndpoint
	probeAPIProviderEndpoint = func(_ context.Context, provider string, cfg agent.ProviderAPIConfig) error {
		if provider != "openrouter" || cfg.APIKey != "profile-test-key" {
			t.Fatalf("unexpected API profile probe: provider=%q key=%q", provider, cfg.APIKey)
		}
		return nil
	}
	t.Cleanup(func() { probeAPIProviderEndpoint = oldProbe })
	fx := newProfileRegisterFixture(t, []RuntimeProfile{{
		ID:             "api-profile",
		WorkspaceID:    "ws-1",
		DisplayName:    "OpenRouter profile",
		ProtocolFamily: "openrouter",
		APIBaseURL:     stringPtr("https://openrouter.example.test/v1"),
		CredentialEnv:  stringPtr("OPENROUTER_API_KEY"),
		DefaultModel:   stringPtr("openai/gpt-4o-mini"),
		Enabled:        true,
	}}, 200)
	fx.daemon.cfg.Agents = map[string]AgentEntry{}

	_, _, _, err := fx.daemon.registerRuntimesForWorkspaceLocked(context.Background(), "ws-1")
	if err != nil {
		t.Fatalf("registerRuntimesForWorkspace: %v", err)
	}
	if len(fx.sentRuntimes) != 1 {
		t.Fatalf("API provider profile was not registered: %v", fx.sentRuntimes)
	}
	if fx.sentRuntimes[0]["type"] != "openrouter" || fx.sentRuntimes[0]["profile_id"] != "api-profile" {
		t.Fatalf("registered API runtime = %v", fx.sentRuntimes[0])
	}
	if len(fx.sentFailures) != 0 {
		t.Fatalf("profile failures = %v, want none", fx.sentFailures)
	}
	entry, ok := fx.daemon.profileAPIEntries["api-profile"]
	if !ok || entry.entry.APIBaseURL != "https://openrouter.example.test/v1" || entry.entry.apiKey != "profile-test-key" || entry.entry.Model != "openai/gpt-4o-mini" {
		t.Fatalf("profile API entry = %+v, want daemon-local resolved config", entry)
	}
}

func stringPtr(value string) *string { return &value }

func setMissingCLIs(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	t.Setenv("SHELL", "/missing/login-shell")
	for _, desc := range agent.ProviderCatalog() {
		if desc.Kind != agent.ProviderKindOpenAICompatible && desc.Kind != agent.ProviderKindOpenCodeAPI {
			continue
		}
		t.Setenv(desc.BaseURLEnv, "")
		t.Setenv(desc.APIKeyEnv, "")
		for _, name := range desc.OptionalKeyEnv {
			t.Setenv(name, "")
		}
		t.Setenv(apiProviderModelEnv(desc.ID), "")
	}
	for _, name := range []string{
		"MULTICA_CLAUDE_PATH", "MULTICA_CODEX_PATH", "MULTICA_OPENCODE_PATH", "MULTICA_DEVECO_PATH",
		"MULTICA_OPENCLAW_PATH", "MULTICA_HERMES_PATH", "MULTICA_PI_PATH", "MULTICA_CURSOR_PATH",
		"MULTICA_COPILOT_PATH", "MULTICA_KIMI_PATH", "MULTICA_REASONIX_PATH", "MULTICA_DSH_PATH",
		"MULTICA_KIRO_PATH", "MULTICA_CODEBUDDY_PATH", "MULTICA_ANTIGRAVITY_PATH", "MULTICA_QODER_PATH",
		"MULTICA_QODERCLICN_PATH", "MULTICA_TRAECLI_PATH", "MULTICA_GROK_PATH", "MULTICA_QWEN_PATH",
		"MULTICA_QWENPAW_PATH", "MULTICA_MCODE_PATH", "MULTICA_DIM_PATH", "MULTICA_ZEROCLAW_PATH",
	} {
		t.Setenv(name, t.TempDir()+"/missing")
	}
}
