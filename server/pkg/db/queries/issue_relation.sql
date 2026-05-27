-- Issue relations / backlinks (ITT-237 Phase 1).
--
-- A row is a directed cross-reference: source_issue_id references
-- target_issue_id. These are data links only and never notify members or
-- trigger agents.

-- name: UpsertIssueRelation :one
-- Idempotent: re-linking the same (source, target, type) returns the existing
-- row instead of creating a duplicate. The no-op DO UPDATE lets RETURNING
-- surface the row on conflict.
INSERT INTO issue_relation (
    workspace_id, source_issue_id, target_issue_id, relation_type,
    created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (source_issue_id, target_issue_id, relation_type)
DO UPDATE SET source_issue_id = EXCLUDED.source_issue_id
RETURNING *;

-- name: DeleteIssueRelation :exec
DELETE FROM issue_relation
WHERE source_issue_id = $1
  AND target_issue_id = $2
  AND relation_type = $3;

-- name: ListIssueRelationsBySource :many
-- Forward references: issues that this issue points to.
SELECT * FROM issue_relation
WHERE source_issue_id = $1
ORDER BY created_at;

-- name: ListIssueRelationsByTarget :many
-- Backlinks: issues that point to this issue.
SELECT * FROM issue_relation
WHERE target_issue_id = $1
ORDER BY created_at;
