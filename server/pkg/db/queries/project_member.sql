-- name: ListPrivateProjectIDs :many
-- Every private project in the workspace: the hidden set for a viewer no
-- workspace member vouches for (an unattributed agent run, a removed member).
SELECT id FROM project
WHERE workspace_id = $1 AND visibility = 'private';

-- name: ListHiddenProjectIDsForUser :many
-- Private projects a regular (non owner/admin) workspace member cannot see.
-- Owners and admins see every project; callers skip this query for them.
SELECT p.id FROM project p
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND p.visibility = 'private'
  AND NOT EXISTS (
      SELECT 1 FROM project_member pm
      WHERE pm.project_id = p.id AND pm.user_id = sqlc.arg('user_id')
  );

-- name: ListPrivateProjectAudience :many
-- Realtime fanout for private-project events: each private project with its
-- member user ids (NULL user_id when the project has no members yet).
SELECT p.id AS project_id, pm.user_id
FROM project p
LEFT JOIN project_member pm ON pm.project_id = p.id
WHERE p.workspace_id = $1 AND p.visibility = 'private';

-- name: ListProjectMembers :many
SELECT pm.workspace_id, pm.project_id, pm.user_id, pm.added_by, pm.created_at,
       u.name AS user_name, u.email AS user_email, u.avatar_url AS user_avatar_url
FROM project_member pm
JOIN "user" u ON u.id = pm.user_id
WHERE pm.project_id = sqlc.arg('project_id') AND pm.workspace_id = sqlc.arg('workspace_id')
ORDER BY pm.created_at ASC, pm.user_id ASC;

-- name: ListProjectMemberUserIDs :many
SELECT user_id FROM project_member
WHERE project_id = sqlc.arg('project_id') AND workspace_id = sqlc.arg('workspace_id');

-- name: AddProjectMembers :many
-- Only current workspace members are inserted; the caller compares the
-- returned rows with its input to reject unknown users.
INSERT INTO project_member (workspace_id, project_id, user_id, added_by)
SELECT m.workspace_id, sqlc.arg('project_id')::uuid, m.user_id, sqlc.narg('added_by')::uuid
FROM member m
WHERE m.workspace_id = sqlc.arg('workspace_id')
  AND m.user_id = ANY(sqlc.arg('user_ids')::uuid[])
ON CONFLICT (project_id, user_id) DO NOTHING
RETURNING *;

-- name: DeleteProjectMember :execrows
DELETE FROM project_member
WHERE project_id = sqlc.arg('project_id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND user_id = sqlc.arg('user_id');

-- name: DeleteProjectMembersByProject :exec
DELETE FROM project_member
WHERE project_id = sqlc.arg('project_id') AND workspace_id = sqlc.arg('workspace_id');

-- name: DeleteProjectMembersByUser :exec
-- A member leaving the workspace leaves every private project in it.
DELETE FROM project_member
WHERE workspace_id = sqlc.arg('workspace_id') AND user_id = sqlc.arg('user_id');

-- name: UpdateProjectVisibility :one
UPDATE project SET visibility = sqlc.arg('visibility'), updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: GetIssueProjectID :one
-- Realtime fanout resolves an event's issue to its project.
SELECT project_id FROM issue WHERE id = $1;

-- name: GetCommentIssueProjectID :one
SELECT i.project_id FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE c.id = $1;

-- name: TouchProjectSearchIndexEntries :exec
-- Re-announce a project's issues and comments to local search sync after its
-- access changed, so clients that lost access receive them as deletions and
-- clients that gained it receive them as upserts.
INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
SELECT 'issue', i.id, i.workspace_id, pg_current_xact_id(), now()
FROM issue i
WHERE i.project_id = sqlc.arg('project_id') AND i.workspace_id = sqlc.arg('workspace_id')
UNION ALL
SELECT 'comment', c.id, c.workspace_id, pg_current_xact_id(), now()
FROM comment c
JOIN issue i ON i.id = c.issue_id
WHERE i.project_id = sqlc.arg('project_id') AND i.workspace_id = sqlc.arg('workspace_id')
ON CONFLICT (entity_type, entity_id) DO UPDATE
    SET workspace_id = EXCLUDED.workspace_id,
        change_xid = EXCLUDED.change_xid,
        changed_at = EXCLUDED.changed_at;

-- name: ListIssueProjectIDs :many
-- Issue -> project for run-history redaction; issues outside a project are
-- returned with a NULL project_id.
SELECT id, project_id FROM issue WHERE id = ANY(sqlc.arg('ids')::uuid[]);
