-- Contribution dashboard. These queries load the raw material — issues in
-- scope and the dated events inside the window — and
-- internal/contribution aggregates them in Go, so every metric on the page
-- derives its counting rules from one place.
--
-- Event queries take no project filter: the Go layer joins each event to the
-- issue set returned by ListContributionIssues, which already carries the
-- project and private-project scope.

-- name: ListContributionIssues :many
-- status_category is '' only for a status key with no catalog row; the Go
-- layer falls back to the built-in key mapping for those.
SELECT
    i.id,
    i.number,
    i.title,
    i.status,
    COALESCE(s.category, '')::text AS status_category,
    i.project_id,
    i.creator_type,
    i.creator_id,
    i.created_at
FROM issue i
LEFT JOIN issue_status s
    ON s.workspace_id = i.workspace_id AND s.key = i.status
WHERE i.workspace_id = @workspace_id::uuid
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.project_id IS NULL OR NOT (i.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])));

-- name: ListContributionStatusChanges :many
-- to_category is the lifecycle category of the status the issue moved TO;
-- '' when that status has no catalog row.
SELECT
    a.issue_id,
    COALESCE(a.actor_type, '')::text AS actor_type,
    a.actor_id,
    a.created_at,
    COALESCE(a.details->>'to', '')::text AS to_status,
    COALESCE(s.category, '')::text AS to_category
FROM activity_log a
LEFT JOIN issue_status s
    ON s.workspace_id = a.workspace_id AND s.key = a.details->>'to'
WHERE a.workspace_id = @workspace_id::uuid
  AND a.action = 'status_changed'
  AND a.issue_id IS NOT NULL
  AND a.created_at >= @since::timestamptz;

-- name: ListContributionComments :many
-- Plain comments and replies only: progress updates, status-change notes and
-- system narration are not collaboration, and deleted comments are gone.
SELECT c.issue_id, c.author_type, c.author_id, c.created_at
FROM comment c
WHERE c.workspace_id = @workspace_id::uuid
  AND c.created_at >= @since::timestamptz
  AND c.type = 'comment'
  AND c.deleted_at IS NULL
  AND c.author_type IN ('member', 'agent');
