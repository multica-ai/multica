package agentvault

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
)

// Access is one row of the per-agent access table: which Agent Vault box this
// agent may use, and at which role. This is the TECH-2962 admin-controlled
// table — the single source of truth for per-agent key access.
type Access struct {
	AgentID   string    `json:"agent_id"`
	Vault     string    `json:"vault"`
	Role      string    `json:"role"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store is the database layer for the per-agent Agent Vault access table.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore builds a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ListForAgent returns the boxes (vaults) a given agent may use.
func (s *Store) ListForAgent(ctx context.Context, workspaceID, agentID string) ([]Access, error) {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace id: %w", err)
	}
	agID, err := util.ParseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("agent id: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT agent_id, vault, role, updated_at
		FROM cerebro_agentvault_agent_access
		WHERE workspace_id = $1 AND agent_id = $2
		ORDER BY vault
	`, wsID, agID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Access
	for rows.Next() {
		var a Access
		var ag pgtype.UUID
		if err := rows.Scan(&ag, &a.Vault, &a.Role, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.AgentID = util.UUIDToString(ag)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccess upserts one (agent, vault) access row at the given role.
func (s *Store) SetAccess(ctx context.Context, workspaceID, agentID, vault, role string) error {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return fmt.Errorf("workspace id: %w", err)
	}
	agID, err := util.ParseUUID(agentID)
	if err != nil {
		return fmt.Errorf("agent id: %w", err)
	}
	if role == "" {
		role = "read-only"
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO cerebro_agentvault_agent_access (workspace_id, agent_id, vault, role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, agent_id, vault)
		DO UPDATE SET role = EXCLUDED.role, updated_at = now()
	`, wsID, agID, vault, role)
	return err
}

// DeleteAccess removes one (agent, vault) access row.
func (s *Store) DeleteAccess(ctx context.Context, workspaceID, agentID, vault string) error {
	wsID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return fmt.Errorf("workspace id: %w", err)
	}
	agID, err := util.ParseUUID(agentID)
	if err != nil {
		return fmt.Errorf("agent id: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		DELETE FROM cerebro_agentvault_agent_access
		WHERE workspace_id = $1 AND agent_id = $2 AND vault = $3
	`, wsID, agID, vault)
	return err
}
