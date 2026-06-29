-- name: ListLabels :many
SELECT * FROM issue_label
WHERE workspace_id = $1
ORDER BY LOWER(name) ASC;

-- name: GetLabel :one
SELECT * FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: CreateLabel :one
INSERT INTO issue_label (workspace_id, name, color, instructions)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateLabel :one
UPDATE issue_label SET
    name = COALESCE(sqlc.narg('name'), name),
    color = COALESCE(sqlc.narg('color'), color),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteLabel :one
-- :one RETURNING id so the handler distinguishes pgx.ErrNoRows (→ 404) from
-- infrastructure errors (→ 500), and avoids a TOCTOU precheck.
DELETE FROM issue_label
WHERE id = $1 AND workspace_id = $2
RETURNING id;

-- name: AttachLabelToIssue :exec
-- Workspace-guarded INSERT: the WHERE EXISTS clauses ensure both the issue
-- and the label belong to the given workspace. A future caller that forgets
-- handler-level prechecks still cannot attach labels across workspaces.
INSERT INTO issue_to_label (issue_id, label_id)
SELECT sqlc.arg('issue_id')::uuid, sqlc.arg('label_id')::uuid
WHERE EXISTS (
    SELECT 1 FROM issue i
    WHERE i.id = sqlc.arg('issue_id')::uuid
      AND i.workspace_id = sqlc.arg('workspace_id')::uuid
)
AND EXISTS (
    SELECT 1 FROM issue_label l
    WHERE l.id = sqlc.arg('label_id')::uuid
      AND l.workspace_id = sqlc.arg('workspace_id')::uuid
)
ON CONFLICT DO NOTHING;

-- name: DetachLabelFromIssue :exec
-- Workspace-guarded DELETE: only deletes if the issue is in the given
-- workspace. Mirror of the attach query.
DELETE FROM issue_to_label
WHERE issue_id = sqlc.arg('issue_id')::uuid
  AND label_id = sqlc.arg('label_id')::uuid
  AND EXISTS (
      SELECT 1 FROM issue i
      WHERE i.id = sqlc.arg('issue_id')::uuid
        AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  );

-- name: ListLabelsByIssue :many
-- Workspace filter at the SQL layer (mirrors GetProjectInWorkspace). Any caller
-- that passes the wrong workspace gets an empty list rather than leaking labels.
SELECT l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = sqlc.arg('issue_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForIssues :many
-- Bulk variant: fetch labels for many issues in one round-trip so the issue
-- list endpoints can fold labels into each row without N+1 queries from the
-- client. Workspace-guarded the same way as ListLabelsByIssue.
SELECT il.issue_id, l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY il.issue_id, LOWER(l.name) ASC;
