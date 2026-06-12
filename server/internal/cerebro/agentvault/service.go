package agentvault

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// tokenMinter mints a per-agent, vault-scoped Agent Vault token. *Client satisfies it.
type tokenMinter interface {
	MintAgentToken(ctx context.Context, agentName string, access []VaultAccess) (string, error)
}

// accessLister returns an agent's granted boxes. *Store satisfies it.
type accessLister interface {
	ListForAgent(ctx context.Context, workspaceID, agentID string) ([]Access, error)
}

// Compile-time guards: the real Client/Store satisfy the broker interfaces.
var (
	_ tokenMinter  = (*Client)(nil)
	_ accessLister = (*Store)(nil)
)

// Service orchestrates claim-time brokering setup: read the per-agent access
// table, mint a scoped Agent Vault token, and produce the spawn env that routes
// the agent through the Multica relay (which holds the internal path).
type Service struct {
	minter   tokenMinter
	lister   accessLister
	relayURL string
	caPath   string
}

// NewService wires the broker. relayURL is the Multica relay proxy base URL the
// agent will use as HTTPS_PROXY; caPath is the on-disk MITM CA PEM path.
func NewService(minter tokenMinter, lister accessLister, relayURL, caPath string) *Service {
	return &Service{minter: minter, lister: lister, relayURL: relayURL, caPath: caPath}
}

// PrepareSpawnEnv returns the env that wires this agent through the broker, or
// nil when the agent has no granted vaults (no brokering for it). Errors are
// returned so the caller fails the claim closed rather than spawning unbrokered.
func (s *Service) PrepareSpawnEnv(ctx context.Context, workspaceID, agentID, agentName string) (SpawnEnv, error) {
	access, err := s.lister.ListForAgent(ctx, workspaceID, agentID)
	if err != nil {
		return nil, fmt.Errorf("agentvault access lookup: %w", err)
	}
	if len(access) == 0 {
		return nil, nil
	}
	token, err := s.minter.MintAgentToken(ctx, agentName, toVaultAccess(access))
	if err != nil {
		return nil, fmt.Errorf("agentvault mint: %w", err)
	}
	proxyURL, err := injectProxyCredential(s.relayURL, token)
	if err != nil {
		return nil, err
	}
	return BuildSpawnEnv(proxyURL, s.caPath), nil
}

func toVaultAccess(access []Access) []VaultAccess {
	out := make([]VaultAccess, 0, len(access))
	for _, a := range access {
		out = append(out, VaultAccess{Vault: a.Vault, Role: a.Role})
	}
	return out
}

// injectProxyCredential puts the token in the relay URL userinfo so HTTP clients
// send it as Proxy-Authorization automatically.
func injectProxyCredential(relayURL, token string) (string, error) {
	relayURL = strings.TrimSpace(relayURL)
	if relayURL == "" || token == "" {
		return "", fmt.Errorf("agentvault: relay url and token both required")
	}
	u, err := url.Parse(relayURL)
	if err != nil {
		return "", fmt.Errorf("agentvault relay url: %w", err)
	}
	u.User = url.User(token)
	return u.String(), nil
}
