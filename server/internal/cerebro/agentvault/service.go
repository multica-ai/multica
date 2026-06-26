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
	// grants is the optional FIR-1739 Part B connector. When set (together with
	// reconcile), the per-agent access table is projected from the authoritative
	// tool-policy chain grants just before it is read, so the table is a derived
	// view of the chain (deny-by-default) rather than an independent authority.
	// nil keeps the prior behaviour (the admin-authored access table is read
	// as-is).
	grants    CredentialGrantSource
	reconcile accessReconciler
}

// NewService wires the broker. relayURL is the Multica relay proxy base URL the
// agent will use as HTTPS_PROXY; caPath is the on-disk MITM CA PEM path.
func NewService(minter tokenMinter, lister accessLister, relayURL, caPath string) *Service {
	return &Service{minter: minter, lister: lister, relayURL: relayURL, caPath: caPath}
}

// SetGrantMirror enables the FIR-1739 Part B credential-grant → access-table
// projection. src yields an agent's authoritative chain grants (resolved to box
// names); reconcile is the access table the projection is written through
// (normally the same *Store this Service reads). Both must be non-nil to take
// effect. Called once at wiring time, so no locking is needed.
func (s *Service) SetGrantMirror(src CredentialGrantSource, reconcile accessReconciler) {
	s.grants = src
	s.reconcile = reconcile
}

// PrepareSpawnEnv returns the env that wires this agent through the broker, or
// nil when the agent has no granted vaults (no brokering for it). Errors are
// returned so the caller fails the claim closed rather than spawning unbrokered.
func (s *Service) PrepareSpawnEnv(ctx context.Context, workspaceID, agentID, agentName string) (SpawnEnv, error) {
	// FIR-1739 Part B: project the authoritative tool-policy chain grants onto
	// the access table before reading it, so brokering reflects the one true
	// model. A reconcile error fails the claim closed (no broker env) — never a
	// stale-grant spawn. No-op when the mirror is not wired.
	if s.grants != nil && s.reconcile != nil {
		if err := reconcileAgentAccess(ctx, s.reconcile, s.grants, workspaceID, agentID); err != nil {
			return nil, err
		}
	}
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
