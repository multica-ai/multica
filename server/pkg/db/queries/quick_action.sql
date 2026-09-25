-- name: ListQuickActions :many
-- One projection for every caller. `private` rows belong to their creator, so
-- they are filtered here rather than in the handler; nothing else is hidden.
-- Permission to actually RUN an action is decided only at run time, so this
-- query does no permission work at all.
--
-- Ordering is use_count DESC everywhere — the list's job is to answer "what
-- does this workspace actually use". LOWER(name) breaks ties so equal counts
-- never render in random order.
SELECT * FROM quick_action
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR status = 'active')
  AND (visibility = 'public' OR created_by_id = sqlc.arg('viewer_id')::uuid)
ORDER BY use_count DESC, LOWER(name) ASC;

-- name: GetQuickAction :one
SELECT * FROM quick_action
WHERE id = $1 AND workspace_id = $2;

-- name: CountActiveQuickActions :one
SELECT COUNT(*) FROM quick_action
WHERE workspace_id = $1 AND status = 'active';

-- name: CreateQuickAction :one
INSERT INTO quick_action (
    workspace_id, name, description, assignee_type, assignee_id, prompt,
    visibility, created_by_type, created_by_id
) VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('name')::text,
    sqlc.arg('description')::text,
    sqlc.arg('assignee_type')::text,
    sqlc.arg('assignee_id')::uuid,
    sqlc.arg('prompt')::text,
    sqlc.arg('visibility')::text,
    sqlc.arg('created_by_type')::text,
    sqlc.arg('created_by_id')::uuid
)
RETURNING *;

-- name: UpdateQuickAction :one
-- COALESCE-on-narg partial update: an omitted field keeps its stored value.
-- assignee_type and assignee_id move together (the handler requires both), so
-- a type swap can never land with a mismatched id.
UPDATE quick_action SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    assignee_type = COALESCE(sqlc.narg('assignee_type'), assignee_type),
    assignee_id = COALESCE(sqlc.narg('assignee_id'), assignee_id),
    prompt = COALESCE(sqlc.narg('prompt'), prompt),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    status = COALESCE(sqlc.narg('status'), status),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteQuickAction :exec
-- workspace_id is a SQL-layer tenant guard, matching DeleteComment.
DELETE FROM quick_action WHERE id = $1 AND workspace_id = $2;

-- name: DeletePrivateQuickActionsByCreator :exec
-- Run when a member leaves or is removed. A private action is visible and
-- runnable ONLY by its creator, so once they are gone nobody can see it, run
-- it, or clean it up — while it still counts against the workspace's active
-- limit. Public actions are workspace furniture and deliberately survive their
-- author's departure.
DELETE FROM quick_action
WHERE workspace_id = $1 AND created_by_id = $2 AND visibility = 'private';

-- name: TouchQuickActionUsage :exec
-- Best-effort usage telemetry, written after a successful run. Deliberately
-- not part of the run transaction: a failed counter bump must never lose the
-- run the user asked for. It also drives the list order, so it is the one
-- place ordering state changes.
UPDATE quick_action
SET use_count = use_count + 1, last_used_at = now()
WHERE id = $1 AND workspace_id = $2;
