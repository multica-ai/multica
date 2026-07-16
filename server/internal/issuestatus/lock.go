package issuestatus

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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

// LockWorkspaceForStatusWrite takes the workspace-scoped, transaction-lifetime
// advisory lock that is THE single serialization point for status writes
// (MUL-4809, plan §5.5). Every catalog mutation — create, rename/recolor,
// default swap, and archive-with-issue-migration — and any future issue-status
// assignment that could point an issue at a status being archived MUST run
// inside a transaction and call this first, so those read-then-write sequences
// stay atomic against each other. The lock is released automatically on commit
// or rollback.
//
// Note this advisory lock protects the catalog invariants (cap, single default,
// archive census) between concurrent catalog writers; the no-FK workspace
// existence guard against a concurrent workspace delete is a separate FOR KEY
// SHARE gate inside the seed/create statements (see EnsureWorkspaceSystemIssueStatuses
// and CreateCustomIssueStatus), matching the workspace delete/create protocol.
func LockWorkspaceForStatusWrite(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))", WorkspaceLockKey(workspaceID)); err != nil {
		return fmt.Errorf("lock workspace for issue-status write: %w", err)
	}
	return nil
}
