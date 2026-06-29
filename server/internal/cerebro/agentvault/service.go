package agentvault

import "context"

// Service keeps the per-agent Agent Vault access table aligned with the
// authoritative tool-policy credential grants (FIR-1739 Part B) at claim time.
//
// FIR-2210: agents no longer reach Agent Vault through an agent-side
// forward-proxy (the HTTPS_PROXY relay was removed — Jesper's rule: agents
// never use a tunnel and never reach Agent Vault directly). Credential access
// now runs through Multica connections (the FIR-2166 server-side connection
// proxy, where the backend injects the credential and the agent never sees it),
// so the broker injects NO agent-side transport env. The per-agent access table
// and its grant projection are retained as the grant model the connections-
// backed path consumes; PrepareSpawnEnv only reconciles that table.
type Service struct {
	// grants is the FIR-1739 Part B connector. When set (together with
	// reconcile), the per-agent access table is projected from the authoritative
	// tool-policy chain grants at claim time, so the table is a derived view of
	// the chain (deny-by-default) rather than an independent authority. nil keeps
	// the admin-authored access table as-is.
	grants    CredentialGrantSource
	reconcile accessReconciler
}

// NewService wires the broker. The grant mirror is attached separately via
// SetGrantMirror.
func NewService() *Service {
	return &Service{}
}

// SetGrantMirror enables the FIR-1739 Part B credential-grant → access-table
// projection. src yields an agent's authoritative chain grants (resolved to box
// names); reconcile is the access table the projection is written through
// (normally the *Store). Both must be non-nil to take effect. Called once at
// wiring time, so no locking is needed.
func (s *Service) SetGrantMirror(src CredentialGrantSource, reconcile accessReconciler) {
	s.grants = src
	s.reconcile = reconcile
}

// PrepareSpawnEnv projects the authoritative tool-policy chain grants onto the
// per-agent access table, then returns a nil env: Agent Vault injects no
// agent-side transport (FIR-2210 — credential access is via Multica
// connections, not a forward-proxy). It is kept on the claim path as the seam
// the connections-backed credential path builds on. A reconcile error is
// returned so the caller fails the claim closed rather than spawning on a stale
// or half-applied grant set. No-op when the mirror is not wired.
func (s *Service) PrepareSpawnEnv(ctx context.Context, workspaceID, agentID, agentName string) (SpawnEnv, error) {
	if s.grants != nil && s.reconcile != nil {
		if err := reconcileAgentAccess(ctx, s.reconcile, s.grants, workspaceID, agentID); err != nil {
			return nil, err
		}
	}
	return nil, nil
}
