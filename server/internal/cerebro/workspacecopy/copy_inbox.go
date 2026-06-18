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
	return InboxCopyResult{InboxIssues: int64(len(ids)), IssuesCopied: copied}, nil
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
