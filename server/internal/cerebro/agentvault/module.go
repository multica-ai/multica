package agentvault

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module bundles the broker pieces wired from runtime config. It is the single
// construction entry point so the upstream router/claim hooks stay tiny.
//
// FIR-2210: the forward-proxy relay was removed — agents reach credentials via
// Multica connections, never a tunnel. The Module now exposes only the
// claim-time grant-reconciling Service and the per-agent access Store.
type Module struct {
	Service *Service
	Store   *Store
}

// NewModule builds the broker from environment config + the DB pool. Returns
// ok=false when Agent Vault is not configured (no admin creds) so callers can
// skip mounting cleanly — the grant-mirror path stays dormant.
func NewModule(pool *pgxpool.Pool) (*Module, bool) {
	if _, ok := LoadConfig(); !ok {
		return nil, false
	}
	store := NewStore(pool)
	svc := NewService()
	return &Module{Service: svc, Store: store}, true
}
