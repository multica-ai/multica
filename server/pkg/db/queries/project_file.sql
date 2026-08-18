-- Project files: the attachment rows that belong to a project's work, surfaced
-- in the project panel's "files" section. "Belongs to a project" means the
-- attachment is scoped through one of the three project-bearing paths:
--   1. directly on an issue of the project (attachment.issue_id),
--   2. on a comment of an issue of the project (attachment.comment_id),
--   3. on a chat session linked to the project (attachment.chat_session_id).
-- comment has no project_id column, so its path joins through issue to reach
-- the project. Every column reference is fully qualified: the correlated subqueries share
-- names (project_id / workspace_id) with the outer scopes, and sqlc rejects
-- unqualified references as ambiguous even where PostgreSQL would not.

-- name: ListProjectFiles :many
-- hidden is per-viewer: the LEFT JOIN matches only rows hidden by the current
-- user, so hidden is true exactly when THIS member hid the file. Personal hide
-- semantics mean every caller's hidden flag is computed against their own id.
SELECT
  sqlc.embed(a),
  (pfh.id IS NOT NULL)::boolean AS hidden
FROM attachment a
LEFT JOIN project_file_hidden pfh
  ON pfh.project_id = sqlc.arg(project_id)
  AND pfh.attachment_id = a.id
  AND pfh.user_id = sqlc.arg(user_id)
WHERE a.workspace_id = sqlc.arg(workspace_id)
  AND (
    a.issue_id IN (
      SELECT i.id FROM issue i
      WHERE i.project_id = sqlc.arg(project_id) AND i.workspace_id = sqlc.arg(workspace_id)
    )
    OR a.comment_id IN (
      SELECT c.id FROM comment c
      JOIN issue i ON i.id = c.issue_id
      WHERE i.project_id = sqlc.arg(project_id) AND i.workspace_id = sqlc.arg(workspace_id)
    )
    OR a.chat_session_id IN (
      SELECT cs.id FROM chat_session cs
      WHERE cs.project_id = sqlc.arg(project_id) AND cs.workspace_id = sqlc.arg(workspace_id)
    )
  )
ORDER BY a.created_at DESC;

-- name: GetProjectAttachment :one
-- Validates that an attachment is in the given project before a hide/unhide
-- write, so a member cannot hide (or unhide) an arbitrary attachment id that
-- happens to live in another project or workspace. Returns no row otherwise.
SELECT sqlc.embed(a)
FROM attachment a
WHERE a.id = sqlc.arg(attachment_id)
  AND a.workspace_id = sqlc.arg(workspace_id)
  AND (
    a.issue_id IN (
      SELECT i.id FROM issue i
      WHERE i.project_id = sqlc.arg(project_id) AND i.workspace_id = sqlc.arg(workspace_id)
    )
    OR a.comment_id IN (
      SELECT c.id FROM comment c
      JOIN issue i ON i.id = c.issue_id
      WHERE i.project_id = sqlc.arg(project_id) AND i.workspace_id = sqlc.arg(workspace_id)
    )
    OR a.chat_session_id IN (
      SELECT cs.id FROM chat_session cs
      WHERE cs.project_id = sqlc.arg(project_id) AND cs.workspace_id = sqlc.arg(workspace_id)
    )
  );

-- name: HideProjectFile :exec
-- Idempotent: re-hiding an already-hidden file is a no-op via the unique index
-- on (project_id, attachment_id, user_id).
INSERT INTO project_file_hidden (workspace_id, project_id, attachment_id, user_id)
VALUES (
  sqlc.arg(workspace_id),
  sqlc.arg(project_id),
  sqlc.arg(attachment_id),
  sqlc.arg(user_id)
)
ON CONFLICT (project_id, attachment_id, user_id) DO NOTHING;

-- name: UnhideProjectFile :exec
-- Idempotent: deleting a non-existent row succeeds with zero rows affected.
DELETE FROM project_file_hidden
WHERE project_id = sqlc.arg(project_id)
  AND attachment_id = sqlc.arg(attachment_id)
  AND user_id = sqlc.arg(user_id);

-- name: DeleteProjectFileHiddenByAttachment :exec
-- App-layer cleanup for the no-FK rule: reaps hidden rows when an attachment
-- is deleted, so the table never accumulates orphans.
DELETE FROM project_file_hidden
WHERE attachment_id = sqlc.arg(attachment_id);
