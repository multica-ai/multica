-- CEREBRO-PATCH(issue-dependencies): blocks / blocked-by / related CRUD + cycle
-- detection for the issue_dependency table (FIR-823). The table ships in
-- 001_init.up.sql but had no queries until now.
--
-- Storage convention: we only ever persist canonical rows.
--   * A directed "X blocks Y" relation is one row (issue_id=X, depends_on=Y, 'blocks').
--     "blocked_by" is the same row read from the other side — never stored separately.
--   * A symmetric "related" relation is stored once with LEAST/GREATEST ordering so
--     (A,B) and (B,A) collapse to a single canonical row.
-- Every write is workspace-guarded at the SQL layer (mirrors issue_label.sql) so a
-- caller that forgets a handler precheck still cannot link issues across workspaces.

-- name: CreateBlocksEdge :exec
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
SELECT sqlc.arg('issue_id')::uuid, sqlc.arg('depends_on_issue_id')::uuid, 'blocks'
WHERE EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('depends_on_issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND NOT EXISTS (
    SELECT 1 FROM issue_dependency d
    WHERE d.type = 'blocks'
      AND d.issue_id = sqlc.arg('issue_id')::uuid
      AND d.depends_on_issue_id = sqlc.arg('depends_on_issue_id')::uuid
);

-- name: DeleteBlocksEdge :exec
DELETE FROM issue_dependency
WHERE type = 'blocks'
  AND issue_id = sqlc.arg('issue_id')::uuid
  AND depends_on_issue_id = sqlc.arg('depends_on_issue_id')::uuid
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = sqlc.arg('issue_id')::uuid
        AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: CreateRelatedEdge :exec
-- Canonical ordering (LEAST/GREATEST) so (A,B) and (B,A) dedupe to one row.
INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
SELECT LEAST(a, b), GREATEST(a, b), 'related'
FROM (SELECT sqlc.arg('issue_a')::uuid AS a, sqlc.arg('issue_b')::uuid AS b) t
WHERE a <> b
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = a AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  )
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = b AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  )
  AND NOT EXISTS (
      SELECT 1 FROM issue_dependency d
      WHERE d.type = 'related'
        AND d.issue_id = LEAST(a, b)
        AND d.depends_on_issue_id = GREATEST(a, b)
  );

-- name: DeleteRelatedEdge :exec
DELETE FROM issue_dependency
WHERE type = 'related'
  AND issue_id = LEAST(sqlc.arg('issue_a')::uuid, sqlc.arg('issue_b')::uuid)
  AND depends_on_issue_id = GREATEST(sqlc.arg('issue_a')::uuid, sqlc.arg('issue_b')::uuid)
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = sqlc.arg('issue_a')::uuid
        AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListBlocks :many
-- Issues that THIS issue blocks (the downstream side of a 'blocks' edge).
SELECT tgt.id, tgt.number, tgt.title, tgt.status
FROM issue_dependency d
JOIN issue tgt ON tgt.id = d.depends_on_issue_id
WHERE d.type = 'blocks'
  AND d.issue_id = sqlc.arg('issue_id')::uuid
  AND tgt.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY tgt.number;

-- name: ListBlockedBy :many
-- Issues that block THIS issue (the upstream side of a 'blocks' edge).
SELECT src.id, src.number, src.title, src.status
FROM issue_dependency d
JOIN issue src ON src.id = d.issue_id
WHERE d.type = 'blocks'
  AND d.depends_on_issue_id = sqlc.arg('issue_id')::uuid
  AND src.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY src.number;

-- name: ListRelated :many
-- Symmetric: return the issue on the other side of the canonical 'related' row.
SELECT other.id, other.number, other.title, other.status
FROM issue_dependency d
JOIN issue other ON other.id = CASE
        WHEN d.issue_id = sqlc.arg('issue_id')::uuid THEN d.depends_on_issue_id
        ELSE d.issue_id
    END
WHERE d.type = 'related'
  AND (d.issue_id = sqlc.arg('issue_id')::uuid OR d.depends_on_issue_id = sqlc.arg('issue_id')::uuid)
  AND other.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY other.number;

-- name: ListBlockedByForIssues :many
-- Bulk variant for the issue-list endpoint: blockers grouped by blocked issue,
-- one round-trip instead of N. Mirrors ListLabelsForIssues.
SELECT d.depends_on_issue_id AS issue_id, src.id, src.number, src.title, src.status
FROM issue_dependency d
JOIN issue src ON src.id = d.issue_id
WHERE d.type = 'blocks'
  AND d.depends_on_issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND src.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY d.depends_on_issue_id, src.number;

-- name: ListBlocksForIssues :many
SELECT d.issue_id AS issue_id, tgt.id, tgt.number, tgt.title, tgt.status
FROM issue_dependency d
JOIN issue tgt ON tgt.id = d.depends_on_issue_id
WHERE d.type = 'blocks'
  AND d.issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND tgt.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY d.issue_id, tgt.number;

-- name: BlocksPathExists :one
-- Walk the 'blocks' graph forward from from_issue_id and report whether
-- to_issue_id is reachable. Used to reject edges that would create a cycle:
-- before adding "A blocks B", check BlocksPathExists(from=B, to=A).
WITH RECURSIVE reachable AS (
    SELECT depends_on_issue_id AS node
    FROM issue_dependency
    WHERE type = 'blocks' AND issue_id = sqlc.arg('from_issue_id')::uuid
    UNION
    SELECT d.depends_on_issue_id
    FROM issue_dependency d
    JOIN reachable r ON d.issue_id = r.node
    WHERE d.type = 'blocks'
)
SELECT EXISTS (
    SELECT 1 FROM reachable WHERE node = sqlc.arg('to_issue_id')::uuid
) AS path_exists;
