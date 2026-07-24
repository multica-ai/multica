// Package issuestatus owns the per-workspace issue-status catalog (MUL-4809).
//
// Phase 1 scope: idempotently seed the 7 built-in system statuses into every
// workspace. Category is the only machine-readable semantics; name / icon /
// color / description are human-facing. The seed is safe to run inside the
// workspace-create transaction and repeatedly during a rolling deploy: the
// underlying statement takes FOR KEY SHARE on the workspace row (so it inserts
// nothing for an already-deleted workspace, never leaving orphans) and uses ON
// CONFLICT (workspace_id, system_key) DO NOTHING against the explicit arbiter
// index. No foreign keys (CLAUDE.md): workspace_id is an application-level
// reference, and DeleteWorkspace sweeps the catalog in the same transaction.
package issuestatus

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Ensure idempotently seeds the built-in system issue statuses for a single
// workspace. Call it on a *db.Queries bound to the workspace-create tx so the
// catalog commits atomically with the workspace, or on a pool-bound Queries
// for backfill. Re-running is a no-op, and seeding a workspace that no longer
// exists is a no-op (the FOR KEY SHARE existence gate inserts nothing).
func Ensure(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID) error {
	if err := q.EnsureWorkspaceSystemIssueStatuses(ctx, workspaceID); err != nil {
		return fmt.Errorf("ensure workspace issue statuses: %w", err)
	}
	return nil
}
