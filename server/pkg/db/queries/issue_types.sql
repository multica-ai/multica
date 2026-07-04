-- name: ListIssueTypes :many
SELECT * FROM issue_types WHERE workspace_id = $1 ORDER BY position ASC, name ASC;

-- name: GetIssueType :one
SELECT * FROM issue_types WHERE id = $1;

-- name: CreateIssueType :one
INSERT INTO issue_types (workspace_id, name, description, icon, color, is_default, position)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateIssueType :one
UPDATE issue_types
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    color = COALESCE(sqlc.narg('color'), color),
    is_default = COALESCE(sqlc.narg('is_default'), is_default),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteIssueType :exec
DELETE FROM issue_types WHERE id = $1;

-- name: SeedDefaultIssueTypes :exec
INSERT INTO issue_types (workspace_id, name, icon, color, is_default, position)
VALUES
  ($1, 'Task', 'check-square', '#6B7280', true, 0),
  ($1, 'Bug', 'bug', '#EF4444', false, 1),
  ($1, 'Feature', 'sparkles', '#8B5CF6', false, 2),
  ($1, 'Story', 'book-open', '#3B82F6', false, 3),
  ($1, 'Creative Brief', 'palette', '#F59E0B', false, 4),
  ($1, 'Content Piece', 'file-text', '#10B981', false, 5),
  ($1, 'Campaign', 'megaphone', '#EC4899', false, 6)
ON CONFLICT (workspace_id, name) DO NOTHING;
