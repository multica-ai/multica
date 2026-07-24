package issuestatus

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrWorkspaceGone is returned by LockWorkspaceForStatusWrite when the workspace
// row no longer exists (a concurrent DeleteWorkspace already committed). Callers
// map it to 404 and must not perform any catalog write. It is distinct from
// pgx.ErrNoRows so the caller can tell "workspace deleted" from an ordinary
// missing-status lookup.
var ErrWorkspaceGone = errors.New("workspace no longer exists")

// WorkspaceLockKey returns the canonical advisory-lock key that serializes every
// write to a workspace's status catalog and every issue-status assignment that
// could race an archive migration (MUL-4809, plan §5.5).
//
// The key is derived from the workspace UUID's canonical byte form (via
// uuidToString), NOT from a raw request string. Two requests that name the same
// workspace with different textual UUID forms — upper vs lower hex, or any other
// formatting — therefore hash to the SAME lock and cannot bypass mutual
// exclusion. Keying on the raw string (as the first cut did) would let two case
// variants take two different advisory locks and run concurrently, breaking the
// count cap, the single-default-per-Category swap, and the archive census.
func WorkspaceLockKey(workspaceID pgtype.UUID) string {
	return "issuestatus:" + uuidToString(workspaceID)
}

// LockWorkspaceForStatusWrite establishes the ONE lock order every status write
// shares (MUL-4809, plan §5.5), in two steps that MUST be the first thing a
// status-write transaction does:
//
//  1. Take FOR KEY SHARE on the workspace row. This is the no-FK existence gate
//     and — critically — it fixes the global lock order. DeleteWorkspace holds
//     the workspace row FOR UPDATE and only then deletes issue_status rows; if a
//     status write instead grabbed status rows first (e.g. the default-swap
//     ClearCategoryDefault) and reached for the workspace row afterwards, the two
//     transactions would deadlock (40P01). Acquiring the workspace row FIRST here
//     means both sides always take workspace -> status rows, so one simply waits
//     for the other. FOR KEY SHARE shares with other status writers (they don't
//     serialize on this) but conflicts with the delete's FOR UPDATE. A missing
//     row means the workspace was already deleted -> ErrWorkspaceGone, before any
//     write.
//  2. Take the workspace-scoped advisory lock. This serializes catalog writers
//     against each other so the count cap, single-default-per-Category swap, and
//     archive census stay atomic. Its key is the canonical workspace UUID (see
//     WorkspaceLockKey), so a differently-formatted UUID can't take a distinct
//     lock and bypass mutual exclusion.
//
// Every catalog mutation (create, rename/recolor, default swap, archive-with-
// migration) and any future issue-status assignment that could point an issue at
// a status being archived calls this first. Both locks release on commit/rollback.
func LockWorkspaceForStatusWrite(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID) error {
	// Step 1: workspace-row existence gate + global lock order.
	var id pgtype.UUID
	err := tx.QueryRow(ctx, "SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE", workspaceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorkspaceGone
	}
	if err != nil {
		return fmt.Errorf("lock workspace row for issue-status write: %w", err)
	}
	// Step 2: catalog-writer serialization.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", WorkspaceLockKey(workspaceID)); err != nil {
		return fmt.Errorf("lock workspace for issue-status write: %w", err)
	}
	return nil
}
