package daemon

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// providerSandboxAllowlist is the per-provider set of API endpoints required
// for the agent CLI to function. A sandbox without these would deny the
// outbound calls the CLI makes to its model provider.
var providerSandboxAllowlist = map[string][]string{
	"claude":  {"api.anthropic.com:443", "statsig.anthropic.com:443"},
	"cursor":  {"api.cursor.com:443", "api2.cursor.sh:443"},
	"gemini":  {"generativelanguage.googleapis.com:443", "oauth2.googleapis.com:443"},
	"copilot": {"api.githubcopilot.com:443", "api.github.com:443", "copilot-proxy.githubusercontent.com:443"},
}

// codex has its own internal sandbox; the daemon does not wrap it.
var providersWithOwnSandbox = map[string]bool{
	"codex": true,
}

// buildSandboxConfig assembles the sandbox configuration to pass to a backend
// for a given provider. It returns nil when the daemon-level sandbox is
// disabled, when the provider runs its own sandbox (e.g. codex), or when the
// platform is non-darwin (the agent package short-circuits to a regular
// exec, but skipping the work in the daemon avoids spurious log noise).
//
// The returned allowlist is the union of:
//   - daemon-wide hosts (cfg.SandboxAllowlist),
//   - provider-specific defaults (model API endpoints),
//   - the Multica server host derived from cfg.ServerBaseURL,
//   - loopback to the daemon health port,
//   - a per-agent override when AgentData.SandboxAllowlist is non-empty.
func (d *Daemon) buildSandboxConfig(provider string, agentData *AgentData) *agent.SandboxConfig {
	if !d.cfg.EnableSandbox {
		return nil
	}
	if providersWithOwnSandbox[provider] {
		return nil
	}

	hosts := make([]string, 0, 16)
	// Loopback to the daemon health port — required for the agent's
	// `multica` CLI calls (e.g. `multica repo checkout`).
	if d.cfg.HealthPort > 0 {
		hosts = append(hosts,
			fmt.Sprintf("127.0.0.1:%d", d.cfg.HealthPort),
			fmt.Sprintf("localhost:%d", d.cfg.HealthPort),
		)
	}
	// Multica server (the daemon talks to it; the agent CLI also talks to
	// it for issue/comment operations). Derived from the configured base
	// URL so a self-hosted server is reachable without manual allowlist
	// edits.
	if hp := serverHostPort(d.cfg.ServerBaseURL); hp != "" {
		hosts = append(hosts, hp)
	}
	// Provider-default endpoints.
	hosts = append(hosts, providerSandboxAllowlist[provider]...)
	// Daemon-wide operator-supplied additions.
	hosts = append(hosts, d.cfg.SandboxAllowlist...)
	// Per-agent override (admin-gated on the server side).
	if agentData != nil {
		hosts = append(hosts, agentData.SandboxAllowlist...)
	}

	return &agent.SandboxConfig{
		Enabled:          true,
		NetworkAllowlist: hosts,
	}
}

// serverHostPort extracts host:port from cfg.ServerBaseURL. A missing port
// is filled in from the URL scheme.
func serverHostPort(serverBaseURL string) string {
	if strings.TrimSpace(serverBaseURL) == "" {
		return ""
	}
	u, err := url.Parse(serverBaseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https", "wss":
			port = "443"
		case "http", "ws":
			port = "80"
		default:
			return ""
		}
	}
	return net.JoinHostPort(host, port)
}
