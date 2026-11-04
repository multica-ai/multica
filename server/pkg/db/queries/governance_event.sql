-- The governance log (spec 4.5). Append-only: there is no UPDATE and no DELETE
-- query for this table except the workspace teardown below, which removes a
-- workspace's log with the workspace (the deletion manifest requires it).
-- TestGovernanceEventHasNoUpdateOrDeleteQuery enforces this.

-- name: InsertGovernanceEvent :one
INSERT INTO governance_event (id, workspace_id, kind, actor_type, actor_id, subject_type, subject_id, summary, details)
VALUES (
    sqlc.arg('id')::uuid,
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('kind')::text,
    sqlc.arg('actor_type')::text,
    sqlc.narg('actor_id')::uuid,
    sqlc.arg('subject_type')::text,
    sqlc.narg('subject_id')::uuid,
    sqlc.arg('summary')::text,
    sqlc.arg('details')::jsonb
)
RETURNING *;

-- name: ListGovernanceEvents :many
-- Newest first. Every filter is optional. The cursor is the (created_at, id) of
-- the last row of the previous page; both are given together or not at all.
SELECT * FROM governance_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type')::text)
  AND (sqlc.narg('subject_id')::uuid IS NULL OR subject_id = sqlc.narg('subject_id')::uuid)
  AND (sqlc.narg('before_created_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('before_created_at')::timestamptz, sqlc.narg('before_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit')::integer;

-- name: DeleteWorkspaceGovernanceEvents :exec
-- Workspace teardown only. This is the table's one DELETE, named here so the
-- append-only test can allow exactly this statement.
DELETE FROM governance_event WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CountGovernanceEvents :one
-- How many entries a CSV export of the same filter would write. The export
-- checks this before it sends a status, so an export over the row cap is
-- refused with 413 instead of cut short (spec 4.9).
SELECT count(*)::bigint FROM governance_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind')::text)
  AND (sqlc.narg('actor_type')::text IS NULL OR actor_type = sqlc.narg('actor_type')::text)
  AND (sqlc.narg('subject_type')::text IS NULL OR subject_type = sqlc.narg('subject_type')::text)
  AND (sqlc.narg('subject_id')::uuid IS NULL OR subject_id = sqlc.narg('subject_id')::uuid);
