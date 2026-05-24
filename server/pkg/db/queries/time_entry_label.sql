-- name: ListTimeEntryLabelsByWorkspace :many
SELECT * FROM time_entry_label
WHERE workspace_id = $1
ORDER BY LOWER(name), name;

-- name: GetTimeEntryLabelInWorkspace :one
SELECT * FROM time_entry_label
WHERE id = $1 AND workspace_id = $2;

-- name: GetTimeEntryLabelByNameInWorkspace :many
SELECT * FROM time_entry_label
WHERE workspace_id = $1 AND LOWER(name) = LOWER($2)
LIMIT 1;

-- name: CreateTimeEntryLabel :one
INSERT INTO time_entry_label (
    workspace_id, name, color
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: UpdateTimeEntryLabel :one
UPDATE time_entry_label
SET name = $2,
    color = $3,
    updated_at = now()
WHERE id = $1 AND workspace_id = $4
RETURNING *;

-- name: DeleteTimeEntryLabel :exec
DELETE FROM time_entry_label
WHERE id = $1 AND workspace_id = $2;

-- name: AddTimeEntryLabel :exec
INSERT INTO time_entry_to_label (
    time_entry_id, label_id
) VALUES (
    $1, $2
)
ON CONFLICT DO NOTHING;

-- name: RemoveTimeEntryLabel :exec
DELETE FROM time_entry_to_label
WHERE time_entry_id = $1 AND label_id = $2;

-- name: ClearTimeEntryLabels :exec
DELETE FROM time_entry_to_label
WHERE time_entry_id = $1;

-- name: ListLabelsForTimeEntry :many
SELECT time_entry_label.*
FROM time_entry_to_label
JOIN time_entry_label ON time_entry_label.id = time_entry_to_label.label_id
WHERE time_entry_to_label.time_entry_id = $1
ORDER BY LOWER(time_entry_label.name), time_entry_label.name;
