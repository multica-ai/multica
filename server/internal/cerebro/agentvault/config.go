// Package agentvault brokers per-agent access to secrets stored in Agent Vault
// (TECH-3196). The Multica backend reaches Agent Vault over an internal path
// (never exposed publicly); which boxes (vaults) an agent may reach is governed
// by the cerebro_agentvault_agent_access table (the TECH-2962 admin-controlled
// access table).
//
// FIR-2478: the read-only vault-box listing (the credential Permissions vault
// picker) no longer authenticates with an Agent Vault admin email/password read
// from the environment. It resolves the workspace's "Agent Vault" REST API
// connection server-side and uses that connection's own Bearer credential, so no
// Agent Vault admin secret lives in the backend environment — everything goes
// through the connection.
package agentvault

import (
	"os"
	"strings"
)

// defaultInternalURL is the Agent Vault management API base URL on the internal
// path. It mirrors the customer-service-mcp.internal pattern: reachable only from
// inside the Multica backend network, never from an agent machine. It doubles as
// the stable identity used to find the workspace's Agent Vault connection — the
// enabled REST API connection whose URL points at this internal host.
const defaultInternalURL = "http://agent-vault.internal:14321"

// Config holds the non-secret settings the backend uses to locate the Agent
// Vault connection. It carries no credentials: the Bearer token comes from the
// resolved "Agent Vault" connection at call time, never from the environment.
type Config struct {
	// InternalURL is the Agent Vault internal endpoint. Its host:port identifies
	// which workspace connection is the Agent Vault connection (the connection
	// whose URL points at this host). Overridable per environment via the
	// non-secret AGENT_VAULT_INTERNAL_URL, defaulting to defaultInternalURL.
	InternalURL string
}

// LoadConfig reads the non-secret Agent Vault settings from the environment.
// There is no failure mode: with nothing set it falls back to the default
// internal URL. Whether box-listing actually works is decided per workspace at
// call time by whether an enabled "Agent Vault" connection exists.
func LoadConfig() Config {
	return Config{
		InternalURL: strings.TrimRight(strings.TrimSpace(envOr("AGENT_VAULT_INTERNAL_URL", defaultInternalURL)), "/"),
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
