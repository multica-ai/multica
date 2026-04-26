-- name: CreateIssueLabel :one
INSERT INTO issue_label (
    workspace_id, name, color, creator_type, creator_id
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: GetIssueLabel :one
SELECT * FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueLabels :many
SELECT * FROM issue_label
WHERE workspace_id = $1
ORDER BY LOWER(name) ASC;

-- name: UpdateIssueLabel :one
UPDATE issue_label SET
    name       = COALESCE(sqlc.narg('name'),  name),
    color      = COALESCE(sqlc.narg('color'), color),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteIssueLabel :exec
DELETE FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: CountIssuesWithLabel :one
SELECT COUNT(*) AS count
FROM issue_to_label
WHERE label_id = $1;

-- name: CountLabelsInWorkspace :one
SELECT COUNT(*) AS count
FROM issue_label
WHERE workspace_id = $1;

-- name: AttachLabelToIssue :exec
INSERT INTO issue_to_label (issue_id, label_id, actor_type, actor_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id, label_id) DO NOTHING;

-- name: DetachLabelFromIssue :exec
DELETE FROM issue_to_label
WHERE issue_id = $1 AND label_id = $2;

-- name: ListLabelsForIssue :many
SELECT l.*
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = $1
ORDER BY LOWER(l.name) ASC;

-- name: ListLabelsForIssues :many
SELECT l.*, il.issue_id AS _issue_id
FROM issue_label l
JOIN issue_to_label il ON il.label_id = l.id
WHERE il.issue_id = ANY($1::uuid[])
ORDER BY il.issue_id, LOWER(l.name) ASC;

-- name: CountLabelsForIssue :one
SELECT COUNT(*) AS count
FROM issue_to_label
WHERE issue_id = $1;
