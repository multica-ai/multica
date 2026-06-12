// Package agentvault brokers per-agent access to secrets stored in Infisical
// Agent Vault (TECH-3196). The Multica backend reaches Agent Vault over an
// internal path (never exposed publicly); it mints a per-agent, vault-scoped
// session token so each agent can USE a secret without ever holding it. Which
// boxes (vaults) an agent may reach is governed by the cerebro_agentvault_agent_access
// table (the TECH-2962 admin-controlled access table).
package agentvault

import (
	"os"
	"strings"
)

// Default internal address of the Agent Vault management API. Mirrors the
// customer-service-mcp.internal pattern: reachable only from inside the
// Multica backend network, never from an agent machine.
const defaultInternalURL = "http://agent-vault.internal:14321"

// Config holds the connection + admin credentials the backend uses to talk to
// Agent Vault over the internal path. Admin credentials are read from the
// environment (injected from Infisical at deploy time) and never surface to an
// agent actor.
type Config struct {
	// InternalURL is the Agent Vault management API base URL on the internal path.
	InternalURL string
	// AdminEmail / AdminPassword authenticate the backend as the instance owner
	// so it can mint per-agent scoped tokens.
	AdminEmail    string
	AdminPassword string
}

// LoadConfig reads Agent Vault config from the environment. Returns the config
// and whether it is sufficiently populated to broker (ok=false when admin
// credentials are absent, so callers can no-op cleanly).
func LoadConfig() (Config, bool) {
	cfg := Config{
		InternalURL:   strings.TrimRight(strings.TrimSpace(envOr("AGENT_VAULT_INTERNAL_URL", defaultInternalURL)), "/"),
		AdminEmail:    strings.TrimSpace(os.Getenv("AGENT_VAULT_ADMIN_EMAIL")),
		AdminPassword: os.Getenv("AGENT_VAULT_ADMIN_PASSWORD"),
	}
	ok := cfg.InternalURL != "" && cfg.AdminEmail != "" && cfg.AdminPassword != ""
	return cfg, ok
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
