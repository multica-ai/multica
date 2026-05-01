-- name: ListLabelsByWorkspace :many
SELECT * FROM issue_label
WHERE workspace_id = $1
ORDER BY LOWER(name), name;

-- name: GetLabelInWorkspace :one
SELECT * FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: GetLabelByNameInWorkspace :many
SELECT * FROM issue_label
WHERE workspace_id = $1 AND LOWER(name) = LOWER($2)
LIMIT 1;

-- name: CreateLabel :one
INSERT INTO issue_label (
    workspace_id, name, color
) VALUES (
    $1, $2, $3
) RETURNING *;

-- name: UpdateLabel :one
UPDATE issue_label
SET name = $2, color = $3
WHERE id = $1 AND workspace_id = $4
RETURNING *;

-- name: DeleteLabel :exec
DELETE FROM issue_label
WHERE id = $1 AND workspace_id = $2;

-- name: AddIssueLabel :exec
INSERT INTO issue_to_label (
    issue_id, label_id
) VALUES (
    $1, $2
)
ON CONFLICT DO NOTHING;

-- name: RemoveIssueLabel :exec
DELETE FROM issue_to_label
WHERE issue_id = $1 AND label_id = $2;

-- name: ListIssueLabels :many
SELECT issue_label.*
FROM issue_to_label
JOIN issue_label ON issue_label.id = issue_to_label.label_id
WHERE issue_to_label.issue_id = $1
ORDER BY LOWER(issue_label.name), issue_label.name;
