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

// NewModule builds the broker from the DB pool. FIR-2478: the grant-reconciling
// Service and per-agent access Store never used Agent Vault admin credentials —
// they operate purely on the DB — so construction no longer gates on any env
// config. Actual claim-time behavior stays gated by the cerebro_agent_vault
// feature flag (see agentvault_claim_cerebro.go).
func NewModule(pool *pgxpool.Pool) *Module {
	return &Module{Service: NewService(), Store: NewStore(pool)}
}
