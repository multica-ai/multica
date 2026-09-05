package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestDaemonAdvertisesTaskIdentityTokens is the other half of the server's
// pre-signing gate: the server withholds tokens from a daemon that does not
// advertise this, so a daemon that injects them must say so or silently stop
// receiving them.
func TestDaemonAdvertisesTaskIdentityTokens(t *testing.T) {
	caps := daemonClientCapabilities()
	found := false
	for _, part := range strings.Split(caps, ",") {
		if strings.TrimSpace(part) == protocol.DaemonCapabilityTaskIdentityTokensV1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("daemonClientCapabilities() = %q, want it to advertise %q",
			caps, protocol.DaemonCapabilityTaskIdentityTokensV1)
	}
}

func TestLayerTaskTokensInjects(t *testing.T) {
	agentEnv := map[string]string{"CODEX_HOME": "/tmp/codex"}
	layerTaskTokens(agentEnv, map[string]string{"BOT_TOKEN_ERP": "jwt-erp"}, nil)

	if got := agentEnv["BOT_TOKEN_ERP"]; got != "jwt-erp" {
		t.Errorf("agentEnv[BOT_TOKEN_ERP] = %q, want jwt-erp", got)
	}
	if got := agentEnv["CODEX_HOME"]; got != "/tmp/codex" {
		t.Errorf("agentEnv[CODEX_HOME] = %q, want it untouched", got)
	}
}

func TestLayerTaskTokensRespectsBlocklist(t *testing.T) {
	agentEnv := map[string]string{"PATH": "/usr/bin"}
	// A misconfigured catalog must not be able to hijack daemon-owned
	// variables even if the server-side name validation is ever relaxed.
	layerTaskTokens(agentEnv, map[string]string{
		"PATH":        "/evil",
		"MULTICA_FOO": "x",
		"BOT_TOKEN_A": "ok",
	}, nil)

	if got := agentEnv["PATH"]; got != "/usr/bin" {
		t.Errorf("agentEnv[PATH] = %q, want the daemon value preserved", got)
	}
	if _, present := agentEnv["MULTICA_FOO"]; present {
		t.Error("agentEnv[MULTICA_FOO] was set, want it blocked")
	}
	if got := agentEnv["BOT_TOKEN_A"]; got != "ok" {
		t.Errorf("agentEnv[BOT_TOKEN_A] = %q, want ok", got)
	}
}

func TestLayerTaskTokensNoopWhenEmpty(t *testing.T) {
	agentEnv := map[string]string{"CODEX_HOME": "/tmp/codex"}
	layerTaskTokens(agentEnv, nil, nil)

	if len(agentEnv) != 1 {
		t.Errorf("agentEnv = %v, want unchanged", agentEnv)
	}
}

// Custom env must still win: it is the documented local-debugging override,
// and layering order is what guarantees it.
func TestCustomEnvOverridesTaskToken(t *testing.T) {
	agentEnv := map[string]string{}
	layerTaskTokens(agentEnv, map[string]string{"BOT_TOKEN_ERP": "from-server"}, nil)
	layerCustomEnvAndHermesHome(agentEnv, map[string]string{"BOT_TOKEN_ERP": "from-custom-env"}, "", nil)

	if got := agentEnv["BOT_TOKEN_ERP"]; got != "from-custom-env" {
		t.Errorf("agentEnv[BOT_TOKEN_ERP] = %q, want custom_env to win", got)
	}
}

// TestCodexShellPolicyAuthorizesTaskTokens pins the Codex-specific half of
// injection. Codex's shell_environment_policy drops any name containing
// KEY/SECRET/TOKEN unless it is explicitly authorized, and task identity tokens
// are named BOT_TOKEN_* by convention — so layering them into the env is not
// enough; a Codex agent's shell tools would still see them empty while the
// server had already audited their issuance.
func TestCodexShellPolicyAuthorizesTaskTokens(t *testing.T) {
	t.Parallel()
	agentEnv := map[string]string{"BOT_TOKEN_ERP": "jwt", "CUSTOM_FLAG": "enabled"}
	taskTokens := map[string]string{"BOT_TOKEN_ERP": "jwt"}

	withTokens := t.TempDir()
	if err := configureCodexTaskShellEnvironment("codex", withTokens, nil, agentEnv, nil, taskTokens, slog.Default()); err != nil {
		t.Fatalf("configureCodexTaskShellEnvironment: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(withTokens, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(data), "BOT_TOKEN_ERP") {
		t.Errorf("include_only missing the task token name:\n%s", data)
	}
	if strings.Contains(string(data), "jwt") {
		t.Errorf("config.toml must carry names only, never a token value:\n%s", data)
	}

	// Presence in the env alone must not authorize a credential-shaped name:
	// that is the guard against inherited daemon secrets, and it stays.
	withoutTokens := t.TempDir()
	if err := configureCodexTaskShellEnvironment("codex", withoutTokens, nil, agentEnv, nil, nil, slog.Default()); err != nil {
		t.Fatalf("configureCodexTaskShellEnvironment: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(withoutTokens, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if strings.Contains(string(data), "BOT_TOKEN_ERP") {
		t.Errorf("a token-shaped name was authorized without being an issued task token:\n%s", data)
	}
}

// TestBlockedEnvKeyMatchesSharedReservedList keeps the daemon's injection
// blocklist and the server's boot-time catalog check on one list. The
// execenv constant is spelled out in pkg/protocol (execenv is daemon-only);
// this is what notices if the two spellings ever part ways.
func TestBlockedEnvKeyMatchesSharedReservedList(t *testing.T) {
	t.Parallel()
	for _, name := range []string{execenv.CursorMcpAuthSourceEnv, "PATH", "codex_home", "MULTICA_TOKEN"} {
		if !isBlockedEnvKey(name) {
			t.Errorf("isBlockedEnvKey(%q) = false, want true", name)
		}
		if !protocol.IsDaemonReservedEnvName(name) {
			t.Errorf("protocol.IsDaemonReservedEnvName(%q) = false, want true", name)
		}
	}
	// HERMES_HOME is deliberately NOT blocked here (custom_env may set it; the
	// overlay wins afterwards), and an ordinary token name is not either.
	for _, name := range []string{"HERMES_HOME", "BOT_TOKEN_ERP"} {
		if isBlockedEnvKey(name) {
			t.Errorf("isBlockedEnvKey(%q) = true, want false", name)
		}
	}
}
