package daemon

import (
	"log/slog"
	"slices"
	"testing"
)

func sandboxTestDaemon(cfg Config) *Daemon {
	return &Daemon{cfg: cfg, logger: slog.Default()}
}

func TestBuildSandboxConfig_DisabledReturnsNil(t *testing.T) {
	d := sandboxTestDaemon(Config{EnableSandbox: false})
	if got := d.buildSandboxConfig("claude", nil); got != nil {
		t.Errorf("expected nil when sandbox disabled, got %+v", got)
	}
}

func TestBuildSandboxConfig_SkipsCodex(t *testing.T) {
	d := sandboxTestDaemon(Config{EnableSandbox: true, HealthPort: 19514})
	if got := d.buildSandboxConfig("codex", nil); got != nil {
		t.Errorf("expected nil for codex (own sandbox), got %+v", got)
	}
}

func TestBuildSandboxConfig_IncludesLoopbackAndProvider(t *testing.T) {
	d := sandboxTestDaemon(Config{
		EnableSandbox: true,
		HealthPort:    19514,
		ServerBaseURL: "https://multica.example.com",
	})
	got := d.buildSandboxConfig("claude", nil)
	if got == nil || !got.Enabled {
		t.Fatal("expected enabled sandbox config")
	}
	for _, want := range []string{
		"127.0.0.1:19514",
		"localhost:19514",
		"multica.example.com:443",
		"api.anthropic.com:443",
	} {
		if !slices.Contains(got.NetworkAllowlist, want) {
			t.Errorf("allowlist missing %q: %v", want, got.NetworkAllowlist)
		}
	}
}

func TestBuildSandboxConfig_MergesPerAgentOverride(t *testing.T) {
	d := sandboxTestDaemon(Config{
		EnableSandbox: true,
		HealthPort:    19514,
	})
	got := d.buildSandboxConfig("claude", &AgentData{
		SandboxAllowlist: []string{"api.openai.com:443"},
	})
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if !slices.Contains(got.NetworkAllowlist, "api.openai.com:443") {
		t.Errorf("per-agent override not merged: %v", got.NetworkAllowlist)
	}
}

func TestBuildSandboxConfig_MergesDaemonAllowlist(t *testing.T) {
	d := sandboxTestDaemon(Config{
		EnableSandbox:    true,
		HealthPort:       19514,
		SandboxAllowlist: []string{"proxy.internal:8080"},
	})
	got := d.buildSandboxConfig("claude", nil)
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if !slices.Contains(got.NetworkAllowlist, "proxy.internal:8080") {
		t.Errorf("daemon allowlist not merged: %v", got.NetworkAllowlist)
	}
}

func TestServerHostPort(t *testing.T) {
	cases := map[string]string{
		"https://multica.example.com":      "multica.example.com:443",
		"http://localhost:8080":            "localhost:8080",
		"ws://localhost:8080/ws":           "localhost:8080",
		"wss://multica.example.com:443/ws": "multica.example.com:443",
		"":                                 "",
		"not-a-url":                        "",
		"ftp://example.com":                "",
	}
	for in, want := range cases {
		if got := serverHostPort(in); got != want {
			t.Errorf("serverHostPort(%q) = %q, want %q", in, got, want)
		}
	}
}
