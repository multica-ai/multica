package connections

// capability.go syncs connections into cerebro_capability so they appear in
// the tool-policy table (TECH-3108). Each connection registers under
// "connection:<name>" in the "Connections" category, owned by the workspace.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncCapability upserts one connection's cerebro_capability row so the
// tool-policy table always shows it. Called after Create and Update.
func SyncCapability(ctx context.Context, pool *pgxpool.Pool, workspaceID pgtype.UUID, c Connection) error {
	key := CapabilityKey(c.Name)
	category := "Connections"
	source := "connection"
	_, err := pool.Exec(ctx, `
		INSERT INTO cerebro_capability
		  (workspace_id, capability_key, title, category, description, source, metadata, last_reported_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, now(), now())
		ON CONFLICT (workspace_id, capability_key) DO UPDATE SET
		  title             = EXCLUDED.title,
		  category          = EXCLUDED.category,
		  description       = EXCLUDED.description,
		  source            = EXCLUDED.source,
		  last_reported_at  = now(),
		  updated_at        = now()
	`, workspaceID, key, c.DisplayName, category, fmt.Sprintf("%s connection (%s)", c.Type, c.URL), source)
	if err != nil {
		return fmt.Errorf("connections: sync capability %q: %w", key, err)
	}
	return nil
}

// DeleteCapability removes the cerebro_capability row for a connection.
// Called after Delete so the row disappears from the tool-policy table.
func DeleteCapability(ctx context.Context, pool *pgxpool.Pool, workspaceID pgtype.UUID, name string) error {
	key := CapabilityKey(name)
	_, err := pool.Exec(ctx, `
		DELETE FROM cerebro_capability
		WHERE workspace_id = $1 AND capability_key = $2
	`, workspaceID, key)
	if err != nil {
		return fmt.Errorf("connections: delete capability %q: %w", key, err)
	}
	return nil
}
