-- name: ListPrivateProjectIDs :many
-- Every private project in the workspace: the hidden set for a viewer no
-- workspace member vouches for (an unattributed agent run, a removed member).
SELECT id FROM project
WHERE workspace_id = $1 AND visibility = 'private';

-- name: ListHiddenProjectIDsForUser :many
-- Private projects a workspace member cannot see: the ones they are not a
-- member of. A workspace role grants no access (MUL-9284).
SELECT p.id FROM project p
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND p.visibility = 'private'
  AND NOT EXISTS (
      SELECT 1 FROM project_member pm
      WHERE pm.project_id = p.id AND pm.user_id = sqlc.arg('user_id')
  );

-- name: ListHiddenProjectIDsForGuest :many
-- Every project a guest (MUL-9420) cannot see: all but the ones it is a
-- member of, whatever their visibility.
SELECT p.id FROM project p
WHERE p.workspace_id = sqlc.arg('workspace_id')
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

-- name: ListWorkspaceProjectIDs :many
-- The ids among project_ids that are projects of this workspace.
SELECT id FROM project
WHERE workspace_id = sqlc.arg('workspace_id') AND id = ANY(sqlc.arg('project_ids')::uuid[]);

-- name: ListGuestProjects :many
-- Every guest's projects in a workspace, for the members list.
SELECT pm.user_id, pm.project_id
FROM project_member pm
JOIN member m ON m.workspace_id = pm.workspace_id AND m.user_id = pm.user_id
WHERE pm.workspace_id = $1 AND m.role = 'guest'
ORDER BY pm.created_at, pm.project_id;

-- name: AddUserToProjects :many
-- A guest's (MUL-9420) projects. Unlike AddProjectMembers this does not
-- require a member row, so an invitation's projects can be written in the
-- same transaction that creates the member.
INSERT INTO project_member (workspace_id, project_id, user_id, added_by)
SELECT p.workspace_id, p.id, sqlc.arg('user_id')::uuid, sqlc.narg('added_by')::uuid
FROM project p
WHERE p.workspace_id = sqlc.arg('workspace_id')
  AND p.id = ANY(sqlc.arg('project_ids')::uuid[])
ON CONFLICT (project_id, user_id) DO NOTHING
RETURNING *;

-- name: RemoveUserFromProjectsExcept :execrows
DELETE FROM project_member
WHERE workspace_id = sqlc.arg('workspace_id')
  AND user_id = sqlc.arg('user_id')
  AND NOT (project_id = ANY(sqlc.arg('keep_project_ids')::uuid[]));

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

-- name: GetProjectAccessManager :one
-- The one project member who manages a private project's access besides the
-- workspace owners and admins on its list (MUL-9284): its lead when that is a
-- person on the list, otherwise its creator when on the list, otherwise its
-- longest-standing member. The lead itself is free to be an agent or nobody,
-- so this is what keeps a project with members from having no person in
-- charge of them. People added together share a join time; among them the
-- one who added the rest comes first. No row when the list is empty.
SELECT pm.user_id
FROM project_member pm
JOIN project p ON p.id = pm.project_id
WHERE pm.project_id = sqlc.arg('project_id') AND pm.workspace_id = sqlc.arg('workspace_id')
ORDER BY COALESCE(p.lead_type = 'member' AND p.lead_id = pm.user_id, false) DESC,
         COALESCE(p.creator_type = 'member' AND p.creator_id = pm.user_id, false) DESC,
         pm.created_at ASC,
         COALESCE(pm.added_by = pm.user_id, false) DESC,
         pm.user_id ASC
LIMIT 1;

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
-- Re-announce a project, its issues and their comments to local search sync
-- after its access changed, so clients that lost access receive them as
-- deletions and clients that gained it receive them as upserts.
INSERT INTO search_index_change (entity_type, entity_id, workspace_id, change_xid, changed_at)
SELECT 'project', p.id, p.workspace_id, pg_current_xact_id(), now()
FROM project p
WHERE p.id = sqlc.arg('project_id') AND p.workspace_id = sqlc.arg('workspace_id')
UNION ALL
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

-- name: ListAutopilotRunProjectIDs :many
SELECT r.id, a.project_id
FROM autopilot_run r
JOIN autopilot a ON a.id = r.autopilot_id
WHERE r.id = ANY(sqlc.arg('ids')::uuid[]);

-- name: CountProjectsUserIsNotMemberOf :one
-- How many of project_ids the user is not a member of yet: setting a guest's
-- projects to these adds that many (MUL-9420).
SELECT count(*)::bigint FROM unnest(sqlc.arg('project_ids')::uuid[]) AS requested(id)
WHERE NOT EXISTS (
    SELECT 1 FROM project_member pm
    WHERE pm.workspace_id = sqlc.arg('workspace_id')
      AND pm.user_id = sqlc.arg('user_id')
      AND pm.project_id = requested.id
);

-- name: CountGuestsNotInProject :one
-- How many of user_ids are guests (MUL-9420) of the workspace who are not
-- members of project_id yet: adding them shares another project with a
-- guest. A project that does not exist yet (the nil uuid) has no members.
SELECT count(*)::bigint FROM member m
WHERE m.workspace_id = sqlc.arg('workspace_id')
  AND m.role = 'guest'
  AND m.user_id = ANY(sqlc.arg('user_ids')::uuid[])
  AND NOT EXISTS (
      SELECT 1 FROM project_member pm
      WHERE pm.project_id = sqlc.arg('project_id')
        AND pm.user_id = m.user_id
  );

-- name: ListAgentIDsSeenByGuest :many
-- The agents a guest (MUL-9420) meets in its projects: as an issue's
-- assignee or creator, a comment's author, a reaction, an activity entry, a
-- subscriber or a project lead. A guest is shown only these, to render their
-- names and avatars, not the workspace's agent directory.
WITH visible_issue AS (
    SELECT i.id, i.assignee_type, i.assignee_id, i.creator_type, i.creator_id
    FROM issue i
    JOIN project_member pm ON pm.project_id = i.project_id
    WHERE pm.workspace_id = sqlc.arg('workspace_id')
      AND pm.user_id = sqlc.arg('user_id')
      AND i.workspace_id = sqlc.arg('workspace_id')
)
SELECT v.assignee_id::uuid AS agent_id FROM visible_issue v
WHERE v.assignee_type = 'agent' AND v.assignee_id IS NOT NULL
UNION
SELECT v.creator_id FROM visible_issue v WHERE v.creator_type = 'agent'
UNION
SELECT c.author_id FROM comment c JOIN visible_issue v ON v.id = c.issue_id
WHERE c.author_type = 'agent'
UNION
SELECT cr.actor_id FROM comment_reaction cr
JOIN comment c ON c.id = cr.comment_id
JOIN visible_issue v ON v.id = c.issue_id
WHERE cr.actor_type = 'agent'
UNION
SELECT r.actor_id FROM issue_reaction r JOIN visible_issue v ON v.id = r.issue_id
WHERE r.actor_type = 'agent'
UNION
SELECT a.actor_id FROM activity_log a JOIN visible_issue v ON v.id = a.issue_id
WHERE a.actor_type = 'agent' AND a.actor_id IS NOT NULL
UNION
SELECT s.user_id FROM issue_subscriber s JOIN visible_issue v ON v.id = s.issue_id
WHERE s.user_type = 'agent'
UNION
SELECT p.lead_id FROM project p
JOIN project_member pm ON pm.project_id = p.id
WHERE pm.workspace_id = sqlc.arg('workspace_id')
  AND pm.user_id = sqlc.arg('user_id')
  AND p.lead_type = 'agent' AND p.lead_id IS NOT NULL;
