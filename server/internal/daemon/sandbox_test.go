package daemon

// CEREBRO-PATCH(daemon-sandbox-test): cerebro modification of upstream file

import (
	"log/slog"
	"slices"
	"strings"
	"testing"

	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
	"github.com/multica-ai/multica/server/pkg/agent/sandbox"
)

func sandboxTestDaemon(cfg Config) *Daemon {
	return &Daemon{cfg: cfg, logger: slog.Default()}
}

func TestBuildSandboxConfig_DisabledReturnsNil(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: false}})
	if got := d.buildSandboxConfig("claude", nil, nil); got != nil {
		t.Errorf("expected nil when sandbox disabled, got %+v", got)
	}
}

func TestBuildSandboxConfig_SkipsCodex(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	if got := d.buildSandboxConfig("codex", nil, nil); got != nil {
		t.Errorf("expected nil for codex (own sandbox), got %+v", got)
	}
}

func TestBuildSandboxConfig_IncludesLoopbackAndProvider(t *testing.T) {
	d := sandboxTestDaemon(Config{
		SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true},
		HealthPort:    19514,
		ServerBaseURL: "https://multica.example.com",
	})
	got := d.buildSandboxConfig("claude", nil, nil)
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
		SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true},
		HealthPort:    19514,
	})
	got := d.buildSandboxConfig("claude", nil, &AgentData{
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
		SandboxConfig: cerebroruntime.SandboxConfig{
			EnableSandbox:    true,
			SandboxAllowlist: []string{"proxy.internal:8080"},
		},
		HealthPort: 19514,
	})
	got := d.buildSandboxConfig("claude", nil, nil)
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if !slices.Contains(got.NetworkAllowlist, "proxy.internal:8080") {
		t.Errorf("daemon allowlist not merged: %v", got.NetworkAllowlist)
	}
}

// boolPtr is a tiny helper for the per-runtime override tests below.
func boolPtr(b bool) *bool { return &b }

// TestBuildSandboxConfig_RuntimeOverrideEnables drives the JEH-418 acceptance
// criterion from the "force on" direction: env says off, runtime says on, the
// runtime wins and the daemon hands a sandbox config to the backend.
func TestBuildSandboxConfig_RuntimeOverrideEnables(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: false}, HealthPort: 19514})
	got := d.buildSandboxConfig("claude", boolPtr(true), nil)
	if got == nil || !got.Enabled {
		t.Fatalf("expected sandbox enabled by per-runtime override, got %+v", got)
	}
}

// TestBuildSandboxConfig_RuntimeOverrideDisables drives the JEH-418 reverse
// case: env says sandbox on, but the runtime is explicitly opted out (e.g.
// Sara's Mac mini where claude OAuth lives in macOS Keychain unreachable from
// the sandbox). The override must beat the env-var default.
func TestBuildSandboxConfig_RuntimeOverrideDisables(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	if got := d.buildSandboxConfig("claude", boolPtr(false), nil); got != nil {
		t.Errorf("expected nil when per-runtime override disables sandbox, got %+v", got)
	}
}

// TestBuildSandboxConfig_NilOverrideUsesEnvDefault confirms the no-override
// path still falls through to cfg.EnableSandbox in both directions, so
// existing runtimes (sandbox_enabled IS NULL) keep their previous behaviour.
func TestBuildSandboxConfig_NilOverrideUsesEnvDefault(t *testing.T) {
	dOn := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	if got := dOn.buildSandboxConfig("claude", nil, nil); got == nil {
		t.Errorf("EnableSandbox=true + nil override: expected sandbox config, got nil")
	}
	dOff := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: false}, HealthPort: 19514})
	if got := dOff.buildSandboxConfig("claude", nil, nil); got != nil {
		t.Errorf("EnableSandbox=false + nil override: expected nil, got %+v", got)
	}
}

// TestBuildSandboxConfig_PopulatesProviderWritePaths guards JEH-405:
// claude (and the other agent CLIs) keep state outside the workdir
// (~/.claude.json, ~/.cursor, …). Without those paths in
// SandboxConfig.WritablePaths the seatbelt profile denies the writes
// and claude exits 0 with empty output. The daemon must populate the
// list per provider.
func TestBuildSandboxConfig_PopulatesProviderWritePaths(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	got := d.buildSandboxConfig("claude", nil, nil)
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if len(got.WritablePaths) == 0 {
		t.Fatal("expected non-empty writable paths for claude")
	}
	hasClaudeJSON := false
	hasClaudeDir := false
	for _, p := range got.WritablePaths {
		if strings.HasSuffix(p, "/.claude.json") {
			hasClaudeJSON = true
		}
		if strings.HasSuffix(p, "/.claude") {
			hasClaudeDir = true
		}
	}
	if !hasClaudeJSON {
		t.Errorf("expected ~/.claude.json in writable paths: %v", got.WritablePaths)
	}
	if !hasClaudeDir {
		t.Errorf("expected ~/.claude in writable paths: %v", got.WritablePaths)
	}
}

// CEREBRO-PATCH(daemon-sandbox-test): JEH-1774 — legacy + new provider
// keychain-access coverage. Pins the per-provider mapping so a future
// refactor cannot accidentally re-enable read-only keychain for new agents.
//
// TestBuildSandboxConfig_LegacyProviderGetsReadOnlyKeychain guards the
// JEH-1774 backward-compat path: claude/cursor/gemini/copilot all
// authenticate via the user's macOS keychain. Until the env-var
// injection work lands, they must keep the legacy read-only access
// mode or they will exit with "Not logged in".
func TestBuildSandboxConfig_LegacyProviderGetsReadOnlyKeychain(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	for _, provider := range []string{"claude", "cursor", "gemini", "copilot"} {
		t.Run(provider, func(t *testing.T) {
			got := d.buildSandboxConfig(provider, nil, nil)
			if got == nil {
				t.Fatal("expected sandbox config")
			}
			if got.KeychainAccess != sandbox.KeychainAccessReadOnly {
				t.Errorf("expected KeychainAccessReadOnly for %s, got %v", provider, got.KeychainAccess)
			}
			if len(got.KeychainItemsAllowed) == 0 {
				t.Errorf("expected at least one allowed keychain item for %s", provider)
			}
		})
	}
}

// TestBuildSandboxConfig_NewProviderGetsKeychainDeny is the JEH-1774
// acceptance criterion for "deny-by-default for new agents". Any provider
// not in providersWithLegacyKeychain must default to KeychainAccessDeny
// so a sandboxed task cannot invoke `security find-generic-password`
// against any keychain item.
func TestBuildSandboxConfig_NewProviderGetsKeychainDeny(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	got := d.buildSandboxConfig("unknown-cli", nil, nil)
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if got.KeychainAccess != sandbox.KeychainAccessDeny {
		t.Errorf("expected KeychainAccessDeny for unknown provider, got %v", got.KeychainAccess)
	}
	if len(got.KeychainItemsAllowed) != 0 {
		t.Errorf("expected empty keychain items for unknown provider, got %v", got.KeychainItemsAllowed)
	}
}

func TestBuildSandboxConfig_NoWritePathsForUnknownProvider(t *testing.T) {
	d := sandboxTestDaemon(Config{SandboxConfig: cerebroruntime.SandboxConfig{EnableSandbox: true}, HealthPort: 19514})
	got := d.buildSandboxConfig("unknown-cli", nil, nil)
	if got == nil {
		t.Fatal("expected sandbox config")
	}
	if len(got.WritablePaths) != 0 {
		t.Errorf("expected empty writable paths for unknown provider, got %v", got.WritablePaths)
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
