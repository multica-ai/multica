-- Issue relations / backlinks (ITT-237 Phase 1).
--
-- A row is a directed cross-reference: source_issue_id references
-- target_issue_id. These are data links only and never notify members or
-- trigger agents. All reads/writes are workspace-scoped to keep the relation
-- graph from ever crossing workspace boundaries.

-- name: UpsertIssueRelation :one
-- Idempotent: re-linking the same (source, target, type) returns the existing
-- row without a redundant UPDATE. INSERT ... DO NOTHING avoids creating dead
-- row versions on repeated saves; the fallback SELECT returns the row already
-- present when the insert was a no-op. Exactly one row is always returned.
WITH inserted AS (
    INSERT INTO issue_relation (
        workspace_id, source_issue_id, target_issue_id, relation_type,
        created_by_type, created_by_id
    )
    VALUES ($1, $2, $3, $4, $5, $6)
    ON CONFLICT (source_issue_id, target_issue_id, relation_type) DO NOTHING
    RETURNING *
)
SELECT * FROM inserted
UNION ALL
SELECT * FROM issue_relation
WHERE workspace_id = $1
  AND source_issue_id = $2
  AND target_issue_id = $3
  AND relation_type = $4
  AND NOT EXISTS (SELECT 1 FROM inserted);

-- name: DeleteIssueRelation :exec
DELETE FROM issue_relation
WHERE workspace_id = $1
  AND source_issue_id = $2
  AND target_issue_id = $3
  AND relation_type = $4;

-- name: ListIssueRelationsBySource :many
-- Forward references: issues that this issue points to.
SELECT * FROM issue_relation
WHERE workspace_id = $1
  AND source_issue_id = $2
ORDER BY created_at;

-- name: ListIssueRelationsByTarget :many
-- Backlinks: issues that point to this issue.
SELECT * FROM issue_relation
WHERE workspace_id = $1
  AND target_issue_id = $2
ORDER BY created_at;
