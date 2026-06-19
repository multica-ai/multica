// CEREBRO-PATCH(workspace-copy): TECH-3766 — "Copy my inbox".
//
// A workspace merge copies entities by type (all issues, all chats, …). But an
// admin migrating their own work usually wants just the handful of issues they
// actually have open — the ones in their inbox — not the entire issue archive
// (thousands of rows). CopyInbox is that shortcut: it copies every distinct
// issue in the caller's inbox into the target.
//
// The set is resolved server-side from inbox_item, mirroring ListInboxFeed's
// WHERE clause (route='inbox', not archived, this recipient), so it matches
// exactly what the inbox page shows and can never silently miss rows the way a
// paged client-side list could — the same bug class this console already fought.
//
// Each distinct issue goes through the normal idempotent CopyIssue, then one
// relink + reference-rewrite pass heals parent/project links and internal text
// references across the batch — exactly like a cascade copy. Non-destructive and
// re-runnable.
//
// CEREBRO-PATCH(workspace-copy): TECH-3785 — landing copied issues in the target
// inbox. Copying the issue rows alone left them in the target's issue archive but
// NOT in the caller's inbox there, so "Copy my inbox" looked like it did nothing.
// After the copy+heal pass, seedTargetInbox places each copied issue back into the
// recipient's inbox in the target workspace — reusing the same 'manually_added'
// inbox row the manual "add to inbox" action creates (inbox/handler.go), so the
// rows render and behave identically. Dedup keeps a re-run from duplicating rows.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// InboxCopyResult summarizes a "copy my inbox" pass.
type InboxCopyResult struct {
	// InboxIssues is the number of distinct issues found in the caller's inbox.
	InboxIssues int64 `json:"inbox_issues"`
	// IssuesCopied is how many of those were newly copied; the rest were already
	// present in the target from an earlier copy (the per-issue copy is idempotent).
	IssuesCopied int64 `json:"issues_copied"`
	// InboxItemsCreated is how many of the copied issues were newly placed into the
	// recipient's inbox in the target workspace. A re-run that finds them already in
	// the target inbox creates none — so this can be lower than InboxIssues without
	// being an error.
	InboxItemsCreated int64 `json:"inbox_items_created"`
}

// CopyInbox copies every distinct issue in the recipient's active inbox
// (route='inbox', not archived) from sourceWorkspace into targetWorkspace, then
// heals parent/project links and internal references across the batch.
func (s *Store) CopyInbox(ctx context.Context, runID, sourceWorkspace, targetWorkspace, recipient pgtype.UUID) (InboxCopyResult, error) {
	ids, err := s.collectInboxIssues(ctx, sourceWorkspace, recipient)
	if err != nil {
		return InboxCopyResult{}, err
	}
	copied, copyErr := s.copyEachIssue(ctx, runID, targetWorkspace, ids, pgtype.UUID{})
	// Always heal what landed, even on a mid-batch failure, so a partially copied
	// inbox is at least linked + rewritten (and a re-run finishes it).
	if err := s.healAfterCascade(ctx, targetWorkspace); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return InboxCopyResult{}, copyErr
	}
	// Land the copies in the recipient's inbox in the target workspace — the whole
	// point of "copy my inbox". Without this the copied issues sit in the target
	// archive and never surface in the inbox the user actually opens.
	seeded, err := s.seedTargetInbox(ctx, targetWorkspace, recipient, ids)
	if err != nil {
		return InboxCopyResult{}, err
	}
	return InboxCopyResult{InboxIssues: int64(len(ids)), IssuesCopied: copied, InboxItemsCreated: seeded}, nil
}

// seedTargetInbox places every copied inbox issue into the recipient's inbox in
// the target workspace, mirroring the manual "add to inbox" action: a
// type='manually_added', route='inbox', severity='info' row authored by the
// recipient. The copied target issue id is resolved through the copy map, and its
// current title is read from the target issue row. Idempotent: an issue that
// already has an active (non-archived) manually_added row for this recipient is
// skipped, so a re-run never duplicates. Returns the number of rows created.
func (s *Store) seedTargetInbox(ctx context.Context, targetWorkspace, recipient pgtype.UUID, sourceIDs []pgtype.UUID) (int64, error) {
	if len(sourceIDs) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO inbox_item
		    (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, actor_type, actor_id, route)
		SELECT m.target_workspace_id, 'member', $2, 'manually_added', 'info', m.target_id, i.title, 'member', $2, 'inbox'
		FROM cerebro_workspace_copy_map m
		JOIN issue i ON i.id = m.target_id
		WHERE m.target_workspace_id = $1
		  AND m.entity_type = 'issue'
		  AND m.source_id = ANY($3)
		  AND NOT EXISTS (
		      SELECT 1 FROM inbox_item x
		      WHERE x.workspace_id = m.target_workspace_id
		        AND x.recipient_type = 'member'
		        AND x.recipient_id = $2
		        AND x.issue_id = m.target_id
		        AND x.type = 'manually_added'
		        AND x.archived = false
		  )`, targetWorkspace, recipient, sourceIDs)
	if err != nil {
		return 0, fmt.Errorf("seed target inbox: %w", err)
	}
	return tag.RowsAffected(), nil
}

// collectInboxIssues returns the distinct issue IDs in the recipient's inbox —
// the same set the inbox page lists. Mirrors ListInboxFeed's WHERE clause so
// "copy my inbox" matches what the user actually sees.
func (s *Store) collectInboxIssues(ctx context.Context, workspace, recipient pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT issue_id
		FROM inbox_item
		WHERE workspace_id = $1
		  AND recipient_type = 'member'
		  AND recipient_id = $2
		  AND archived = false
		  AND route = 'inbox'
		  AND issue_id IS NOT NULL`, workspace, recipient)
	if err != nil {
		return nil, fmt.Errorf("collect inbox issues: %w", err)
	}
	return scanUUIDs(rows)
}
