package inboxv2

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ReconcileReport is what one pass did.
type ReconcileReport struct {
	// SourcesClaimed is history the write path deferred or a rollback window
	// left behind.
	SourcesClaimed int
	// GroupsRepaired is groups whose rows had moved without them — the only
	// direction in which inbox_item is authoritative.
	GroupsRepaired int
	// OrphansRemoved is groups whose every event has been deleted.
	OrphansRemoved int
}

// Reconcile brings groups and rows back into agreement.
//
// Two gaps to close, both produced by the same thing — a write that reached
// inbox_item without reaching inbox_group:
//
//   - INSERT gaps: rows with no group at all. Produced by a delivery while the
//     write gate was closed, by the oversized-source downgrade, and by any
//     rollback window. Claimed here.
//   - UPDATE gaps: rows whose read/archived no longer match their group.
//     Produced by a v1 endpoint running with the adapters disabled. Repaired
//     here, with the ROWS winning — they are what the user actually saw and
//     acted on.
//
// Idempotent and re-runnable by construction: both halves are defined by a
// predicate over current state, not by a log of what has already been done, so
// a second pass over a healthy inbox finds nothing and writes nothing.
func (s *Service) Reconcile(ctx context.Context, workspaceID pgtype.UUID, batch int32, now time.Time) (ReconcileReport, error) {
	var report ReconcileReport

	users, err := s.q.ListRecipientsWithUnclaimedInboxItems(ctx, batch)
	if err != nil {
		return report, fmt.Errorf("inboxv2: reconcile: list recipients: %w", err)
	}
	for _, user := range users {
		sources, err := s.q.ListUnclaimedInboxSources(ctx, user)
		if err != nil {
			return report, fmt.Errorf("inboxv2: reconcile: list sources: %w", err)
		}
		for _, src := range sources {
			if workspaceID.Valid && src.WorkspaceID != workspaceID {
				continue
			}
			if err := s.migrateSource(ctx, user, src, now); err != nil {
				return report, err
			}
			report.SourcesClaimed++
		}
	}

	drifted, err := s.q.ListInboxGroupsWithRowDrift(ctx, db.ListInboxGroupsWithRowDriftParams{
		WorkspaceID: workspaceID,
		PageSize:    batch,
	})
	if err != nil {
		return report, fmt.Errorf("inboxv2: reconcile: list drift: %w", err)
	}
	for _, g := range drifted {
		if err := s.repairGroup(ctx, g, now); err != nil {
			return report, err
		}
		report.GroupsRepaired++
	}

	if workspaceID.Valid {
		removed, err := s.q.DeleteOrphanInboxGroups(ctx, workspaceID)
		if err != nil {
			return report, fmt.Errorf("inboxv2: reconcile: orphans: %w", err)
		}
		report.OrphansRemoved = int(removed)
	}

	if report.SourcesClaimed > 0 || report.GroupsRepaired > 0 || report.OrphansRemoved > 0 {
		slog.Info("inboxv2: reconcile pass",
			"sources_claimed", report.SourcesClaimed,
			"groups_repaired", report.GroupsRepaired,
			"orphans_removed", report.OrphansRemoved)
	}
	return report, nil
}

// repairGroup recomputes one group from its rows and pushes the result back.
//
// The refresh afterwards is not redundant. Recomputing can change which row is
// the representative, and the mirror encodes that choice — without the second
// half the group would be right and the rows the v1 clients read would still be
// wrong, which is the exact failure being repaired.
func (s *Service) repairGroup(ctx context.Context, g db.ListInboxGroupsWithRowDriftRow, now time.Time) error {
	tx, err := s.tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("inboxv2: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	if _, err := q.LockInboxGroup(ctx, db.LockInboxGroupParams{
		ID: g.ID, WorkspaceID: g.WorkspaceID, RecipientID: g.RecipientID,
	}); err != nil {
		return fmt.Errorf("inboxv2: reconcile: lock group: %w", err)
	}
	if _, err := q.ReconcileInboxGroupFromRows(ctx, db.ReconcileInboxGroupFromRowsParams{
		ID: g.ID, Now: pgtype.Timestamptz{Time: now, Valid: true},
	}); err != nil {
		return fmt.Errorf("inboxv2: reconcile: repair group: %w", err)
	}
	if _, err := q.RefreshInboxItemMirror(ctx, g.ID); err != nil {
		return fmt.Errorf("inboxv2: reconcile: refresh mirror: %w", err)
	}
	return tx.Commit(ctx)
}

// PurgeIssue removes the groups for a deleted issue.
func (s *Service) PurgeIssue(ctx context.Context, workspaceID, issueID pgtype.UUID) error {
	_, err := s.q.DeleteInboxGroupsForIssue(ctx, db.DeleteInboxGroupsForIssueParams{
		WorkspaceID: workspaceID, SourceID: issueID,
	})
	return err
}

// PurgeMember removes the groups of someone who left the workspace.
func (s *Service) PurgeMember(ctx context.Context, workspaceID, recipientID pgtype.UUID) error {
	_, err := s.q.DeleteInboxGroupsForMember(ctx, db.DeleteInboxGroupsForMemberParams{
		WorkspaceID: workspaceID, RecipientID: recipientID,
	})
	return err
}
