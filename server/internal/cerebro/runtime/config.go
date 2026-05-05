// Package runtime holds cerebro-fork-only daemon/runtime configuration that
// extends the upstream daemon Config via Go struct embedding.
//
// CEREBRO-PATCH(daemon-config): the upstream `daemon.Config` struct embeds
// `SandboxConfig` from this package. Field-access (cfg.EnableSandbox,
// cfg.SandboxAllowlist) continues to work transparently because of Go's
// promoted-field rule, so existing daemon code in `server/internal/daemon/`
// and `server/internal/cerebro/sandbox/` does not change.
//
// Origin: PR #16 (feat(agent): macOS sandbox-exec wrapper for spawned agent
// processes). Lives here so future upstream merges of `daemon/config.go`
// touch a single embedded line instead of dozens of additive lines.
package runtime

import (
	"os"
	"runtime"
	"strings"
)

// SandboxConfig is the cerebro-fork sandbox addition to the daemon Config.
// Embedded into daemon.Config so callers may continue to access
// `cfg.EnableSandbox` / `cfg.SandboxAllowlist` directly.
type SandboxConfig struct {
	// EnableSandbox toggles the macOS sandbox-exec wrapper for spawned
	// agent processes. Default true on darwin, false elsewhere.
	EnableSandbox bool
	// SandboxAllowlist is the daemon-level outbound network allowlist
	// (host:port pairs) that the seatbelt profile permits. Loopback to
	// HealthPort and the Multica server host are auto-added at spawn time.
	SandboxAllowlist []string
}

// LoadSandboxConfig reads cerebro sandbox settings from environment variables.
// Keeps loading-logic next to the field declarations so future refactors of
// upstream's LoadConfig only need to call this single helper.
func LoadSandboxConfig() SandboxConfig {
	// Sandbox: enabled by default on darwin (kernel-level Seatbelt), no-op
	// elsewhere. The env var lets operators force-disable it for debugging
	// or force-enable it on non-darwin platforms (where it currently has no
	// effect — the agent.prepareCommand helper short-circuits to a regular
	// exec on non-darwin).
	enableSandbox := runtime.GOOS == "darwin"
	switch strings.TrimSpace(os.Getenv("MULTICA_ENABLE_SANDBOX")) {
	case "false", "0", "no", "off":
		enableSandbox = false
	case "true", "1", "yes", "on":
		enableSandbox = true
	}
	return SandboxConfig{
		EnableSandbox:    enableSandbox,
		SandboxAllowlist: splitCSV(os.Getenv("MULTICA_SANDBOX_NETWORK_ALLOWLIST")),
	}
}

// splitCSV trims and splits a comma-separated list, dropping empty entries.
func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
