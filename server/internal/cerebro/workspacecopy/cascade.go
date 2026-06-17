// CEREBRO-PATCH(workspace-copy): TECH-3732 — cascade copy ("take everything
// underneath with it").
//
// Per-item copy moves one entity at a time; the hierarchy is healed afterwards.
// For a real workspace merge that means copying hundreds of issues by hand. The
// cascade copiers fan a single pick out to its whole subtree:
//
//   - CopyIssueCascade:   the issue + every open descendant sub-issue (any depth).
//   - CopyProjectCascade: the project + every open issue in it, plus those
//                         issues' open descendant sub-issues.
//
// "Open" excludes done/cancelled issues, which stay in the source archive (see
// README "Excluded"). The picked root issue is always copied regardless of its
// own status. Each child goes through the normal idempotent CopyIssue, then a
// single relink + reference-rewrite pass heals links and text across the batch.
package workspacecopy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// CopyIssueCascade copies sourceIssue and all of its open descendant sub-issues
// into the target, then heals parent/project links and internal references.
// CascadeCopied on the returned root counts the descendants newly copied.
func (s *Store) CopyIssueCascade(ctx context.Context, runID, targetWorkspace, sourceIssue pgtype.UUID) (CopyResult, error) {
	ids, err := s.collectIssueSubtree(ctx, sourceIssue)
	if err != nil {
		return CopyResult{}, err
	}
	root, err := s.CopyIssue(ctx, runID, targetWorkspace, sourceIssue)
	if err != nil {
		return CopyResult{}, err
	}
	copied, copyErr := s.copyEachIssue(ctx, runID, targetWorkspace, ids, sourceIssue)
	// Always heal what landed, even on a mid-batch failure, so a partially
	// copied cascade is at least linked + rewritten (and a re-run finishes it).
	if err := s.healAfterCascade(ctx, targetWorkspace); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return CopyResult{}, copyErr
	}
	root.CascadeCopied = copied
	return root, nil
}

// CopyProjectCascade copies sourceProject and every open issue in it (plus those
// issues' open descendant sub-issues), then heals links and references.
func (s *Store) CopyProjectCascade(ctx context.Context, runID, targetWorkspace, sourceProject pgtype.UUID) (CopyResult, error) {
	root, err := s.CopyProject(ctx, runID, targetWorkspace, sourceProject)
	if err != nil {
		return CopyResult{}, err
	}
	ids, err := s.collectProjectIssues(ctx, sourceProject)
	if err != nil {
		return CopyResult{}, err
	}
	copied, copyErr := s.copyEachIssue(ctx, runID, targetWorkspace, ids, pgtype.UUID{})
	// Always heal what landed, even on a mid-batch failure (see CopyIssueCascade).
	if err := s.healAfterCascade(ctx, targetWorkspace); err != nil && copyErr == nil {
		copyErr = err
	}
	if copyErr != nil {
		return CopyResult{}, copyErr
	}
	root.CascadeCopied = copied
	return root, nil
}

// copyEachIssue copies every id in the list (skipping skipRoot, already copied by
// the caller) via the idempotent CopyIssue, returning the count newly copied.
func (s *Store) copyEachIssue(ctx context.Context, runID, targetWorkspace pgtype.UUID, ids []pgtype.UUID, skipRoot pgtype.UUID) (int64, error) {
	var copied int64
	for _, id := range ids {
		if skipRoot.Valid && id == skipRoot {
			continue
		}
		r, err := s.CopyIssue(ctx, runID, targetWorkspace, id)
		if err != nil {
			return copied, fmt.Errorf("cascade copy issue %s: %w", uuid.UUID(id.Bytes), err)
		}
		if !r.AlreadyDone {
			copied++
		}
	}
	return copied, nil
}

// healAfterCascade runs the relink + reference-rewrite post-passes once after a
// batch copy, so order-independent parent/project links and text references all
// resolve to the copies.
func (s *Store) healAfterCascade(ctx context.Context, targetWorkspace pgtype.UUID) error {
	if _, err := s.RelinkIssueRelations(ctx, targetWorkspace); err != nil {
		return err
	}
	if _, err := s.RewriteInternalReferences(ctx, targetWorkspace); err != nil {
		return err
	}
	return nil
}

// collectIssueSubtree returns the root plus every open descendant sub-issue (any
// depth). The root is included regardless of status; descendants that are
// done/cancelled — and their subtrees — are pruned.
func (s *Store) collectIssueSubtree(ctx context.Context, root pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM issue WHERE id = $1
			UNION
			SELECT i.id FROM issue i
			JOIN sub ON i.parent_issue_id = sub.id
			WHERE i.status NOT IN ('done', 'cancelled')
		)
		SELECT id FROM sub`, root)
	if err != nil {
		return nil, fmt.Errorf("collect issue subtree: %w", err)
	}
	return scanUUIDs(rows)
}

// collectProjectIssues returns every open issue whose project_id is the source
// project, plus those issues' open descendant sub-issues (which may themselves
// sit outside the project).
func (s *Store) collectProjectIssues(ctx context.Context, project pgtype.UUID) ([]pgtype.UUID, error) {
	rows, err := s.pool.Query(ctx, `
		WITH RECURSIVE sub AS (
			SELECT id FROM issue
			WHERE project_id = $1 AND status NOT IN ('done', 'cancelled')
			UNION
			SELECT i.id FROM issue i
			JOIN sub ON i.parent_issue_id = sub.id
			WHERE i.status NOT IN ('done', 'cancelled')
		)
		SELECT id FROM sub`, project)
	if err != nil {
		return nil, fmt.Errorf("collect project issues: %w", err)
	}
	return scanUUIDs(rows)
}

func scanUUIDs(rows interface {
	Next() bool
	Scan(...any) error
	Close()
	Err() error
}) ([]pgtype.UUID, error) {
	defer rows.Close()
	var ids []pgtype.UUID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ids: %w", err)
	}
	return ids, nil
}
