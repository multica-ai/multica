package agentvault

import (
	"context"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultProxyAddr is the Agent Vault MITM proxy on the internal path. Separate
// from the management URL (14321): the relay chains CONNECT here (14322).
const defaultProxyAddr = "agent-vault.internal:14322"

// Module bundles the broker pieces wired from runtime config. It is the single
// construction entry point so the upstream router/claim hooks stay tiny.
type Module struct {
	Service *Service
	Relay   *Relay
	Store   *Store
}

// NewModule builds the broker from environment config + the DB pool. Returns
// ok=false when Agent Vault is not configured (no admin creds) so callers can
// skip mounting cleanly. The relay path is also feature-flag gated upstream.
func NewModule(pool *pgxpool.Pool) (*Module, bool) {
	cfg, ok := LoadConfig()
	if !ok {
		return nil, false
	}
	store := NewStore(pool)
	client := NewClient(cfg)
	relayURL := strings.TrimSpace(envOr("AGENT_VAULT_RELAY_URL", ""))
	caPath := strings.TrimSpace(os.Getenv("AGENT_VAULT_CA_PATH"))
	svc := NewService(client, store, relayURL, caPath)

	proxyAddr := strings.TrimSpace(envOr("AGENT_VAULT_PROXY_ADDR", defaultProxyAddr))
	relay := NewRelay(proxyAddr, identityResolver{})
	return &Module{Service: svc, Relay: relay, Store: store}, true
}

// identityResolver treats the agent's proxy credential as the (claim-time
// minted) Agent Vault token itself — the token is scoped no-access/proxy-only
// and cannot reveal secrets, so carrying it to the relay is safe. The relay's
// job is purely to bridge the internal path the agent machine cannot reach.
type identityResolver struct{}

func (identityResolver) ResolveAgentVaultToken(_ context.Context, cred string) (string, bool) {
	cred = strings.TrimSpace(cred)
	return cred, cred != ""
}
