package handler

import (
	"context"
	"os"

	"github.com/jackc/pgx/v5/pgtype"
)

// Reconcile persisted readiness with the checkout on this server. Close the
// reader before writes, and compare updated_at to avoid racing a new sync.
func (h *Handler) reconcileCodeGraphCheckouts(ctx context.Context, workspaceID pgtype.UUID) error {
	rows, err := h.DB.Query(ctx, `SELECT id, updated_at, active_build_id FROM code_graph_repository WHERE workspace_id = $1 AND status = 'ready'`, workspaceID)
	if err != nil {
		return err
	}
	type missingCheckout struct {
		id      pgtype.UUID
		updated pgtype.Timestamptz
		build   pgtype.UUID
	}
	missing := []missingCheckout{}
	for rows.Next() {
		var entry missingCheckout
		if err := rows.Scan(&entry.id, &entry.updated, &entry.build); err != nil {
			rows.Close()
			return err
		}
		dir := codeGraphCheckoutDir(uuidToString(workspaceID), uuidToString(entry.id))
		if entry.build.Valid {
			dir = codeGraphBuildDir(uuidToString(workspaceID), uuidToString(entry.id), uuidToString(entry.build))
		}
		info, err := os.Stat(dir)
		if os.IsNotExist(err) || (err == nil && !info.IsDir()) {
			missing = append(missing, entry)
		}
		if err != nil && !os.IsNotExist(err) {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, entry := range missing {
		if _, err := h.DB.Exec(ctx, `UPDATE code_graph_repository
   SET status = 'failed', internal_status = 'checkout_missing',
       sync_error = 'Checkout missing; synchronize the repository to restore code access.', updated_at = now()
   WHERE id = $1 AND workspace_id = $2 AND status = 'ready' AND updated_at = $3`, entry.id, workspaceID, entry.updated); err != nil {
			return err
		}
	}
	return nil
}
