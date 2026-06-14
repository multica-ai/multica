-- name: ListIssueTypes :many
SELECT * FROM issue_type
WHERE workspace_id = $1
  AND (COALESCE(sqlc.narg('include_archived')::bool, false) = true OR archived_at IS NULL)
ORDER BY position ASC, created_at ASC;

-- name: GetIssueType :one
SELECT * FROM issue_type
WHERE id = $1 AND workspace_id = $2;

-- name: GetIssueTypeByKey :one
SELECT * FROM issue_type
WHERE workspace_id = $1 AND key = $2;

-- name: CreateIssueType :one
INSERT INTO issue_type (workspace_id, key, name, description, color, icon, load_profile, position)
VALUES (@workspace_id, @key, @name, @description, @color, @icon, @load_profile, @position)
RETURNING *;

-- name: UpdateIssueType :one
UPDATE issue_type
SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    color = COALESCE(sqlc.narg('color'), color),
    icon = COALESCE(sqlc.narg('icon'), icon),
    load_profile = COALESCE(sqlc.narg('load_profile'), load_profile),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: ArchiveIssueType :one
UPDATE issue_type
SET archived_at = COALESCE(archived_at, now()), updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: EnsureDefaultIssueTypes :exec
INSERT INTO issue_type (workspace_id, key, name, description, color, icon, load_profile, is_system, position)
VALUES
    (@workspace_id, 'task', 'Task', 'Default execution task.', 'slate', 'check-circle', 'neutral', true, 10),
    (@workspace_id, 'feature', 'Feature', 'Feature or delivery work.', 'blue', 'sparkles', 'deep_work', true, 20),
    (@workspace_id, 'bug', 'Bug', 'Defect investigation or fix.', 'red', 'bug', 'deep_work', true, 30),
    (@workspace_id, 'chore', 'Chore', 'Maintenance, cleanup, or operational work.', 'amber', 'wrench', 'light_work', true, 40),
    (@workspace_id, 'research', 'Research', 'Research, clarification, or exploration.', 'violet', 'search', 'light_work', true, 50),
    (@workspace_id, 'recovery', 'Recovery', 'Recovery, load reduction, or energy restoration.', 'emerald', 'battery', 'recovery', true, 60)
ON CONFLICT (workspace_id, key) DO NOTHING;
