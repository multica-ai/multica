-- Owner recovery for private projects (MUL-9284).
--
-- These are the only queries that read private projects without filtering by
-- membership. They back the workspace owner's list of every private project,
-- and must never back a regular read.

-- name: ListPrivateProjects :many
-- Every private project in the workspace, whoever its members are.
SELECT * FROM project
WHERE workspace_id = $1 AND visibility = 'private'
ORDER BY created_at DESC, id DESC;

-- name: ListPrivateProjectMembers :many
-- The members of every private project in the workspace.
SELECT pm.project_id, pm.user_id, pm.created_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM project_member pm
JOIN project p ON p.id = pm.project_id AND p.workspace_id = pm.workspace_id
JOIN "user" u ON u.id = pm.user_id
WHERE pm.workspace_id = $1 AND p.visibility = 'private'
ORDER BY pm.project_id, pm.created_at ASC, pm.user_id ASC;
