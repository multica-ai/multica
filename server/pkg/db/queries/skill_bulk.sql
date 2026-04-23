-- Skill Bulk Operations - Cross-workspace skill management

-- name: ListAllSkillsForUser :many
-- Lists all skills from all workspaces where the user is a member
SELECT s.* FROM skill s
JOIN member m ON m.workspace_id = s.workspace_id
WHERE m.user_id = $1
ORDER BY s.name ASC, s.workspace_id;

-- name: GetSkillByNameInWorkspace :one
-- Get a skill by its name within a specific workspace
SELECT * FROM skill
WHERE workspace_id = $1 AND name = $2;

-- name: GetSkillWithWorkspaceName :one
-- Get skill with workspace info for display
SELECT s.*, w.name as workspace_name, w.slug as workspace_slug
FROM skill s
JOIN workspace w ON w.id = s.workspace_id
WHERE s.id = $1;

-- name: ListSkillFilesWithContent :many
-- Get all files for a skill with their content
SELECT sf.* FROM skill_file sf
WHERE sf.skill_id = $1
ORDER BY sf.path ASC;

-- name: GetSkillsByNameAcrossWorkspaces :many
-- Find all skills with the same name across user's workspaces
SELECT s.*, w.name as workspace_name, w.slug as workspace_slug
FROM skill s
JOIN workspace w ON w.id = s.workspace_id
JOIN member m ON m.workspace_id = s.workspace_id
WHERE s.name = $1 AND m.user_id = $2
ORDER BY w.name ASC;

-- name: CopySkillToWorkspace :one
-- Copy a skill to another workspace (returns existing if name conflict and overwrite=false)
INSERT INTO skill (workspace_id, name, description, content, config, created_by)
SELECT $2, s.name, s.description, s.content, s.config, $3
FROM skill s
WHERE s.id = $1
ON CONFLICT (workspace_id, name) DO UPDATE SET
    description = CASE WHEN $4 = true THEN EXCLUDED.description ELSE skill.description END,
    content = CASE WHEN $4 = true THEN EXCLUDED.content ELSE skill.content END,
    config = CASE WHEN $4 = true THEN EXCLUDED.config ELSE skill.config END,
    updated_at = now()
WHERE $4 = true
RETURNING *;

-- name: CopySkillFiles :exec
-- Copy files from one skill to another
INSERT INTO skill_file (skill_id, path, content)
SELECT $2, sf.path, sf.content
FROM skill_file sf
WHERE sf.skill_id = $1
ON CONFLICT (skill_id, path) DO UPDATE SET
    content = EXCLUDED.content,
    updated_at = now();

-- name: DeleteSkillFiles :exec
-- Delete all files for a skill
DELETE FROM skill_file sf WHERE sf.skill_id = $1;

-- name: ListUserWorkspacesWithSkills :many
-- Get all workspaces for a user with their skill count
SELECT w.*, COUNT(s.id) as skill_count
FROM workspace w
JOIN member m ON m.workspace_id = w.id
LEFT JOIN skill s ON s.workspace_id = w.id
WHERE m.user_id = $1
GROUP BY w.id
ORDER BY w.name ASC;

-- name: GetWorkspaceMembership :one
-- Check if user is member of workspace and get their role
SELECT m.role FROM member m
WHERE m.workspace_id = $1 AND m.user_id = $2;
