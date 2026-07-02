-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.issue_prefix, w.issue_counter, w.avatar_url
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: GetWorkspace :one
SELECT * FROM workspace
WHERE id = $1;

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspace
WHERE slug = $1;

-- name: CreateWorkspace :one
INSERT INTO workspace (name, slug, description, context, issue_prefix)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateWorkspace :one
UPDATE workspace SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    context = COALESCE(sqlc.narg('context'), context),
    settings = COALESCE(sqlc.narg('settings'), settings),
    repos = COALESCE(sqlc.narg('repos'), repos),
    issue_prefix = COALESCE(sqlc.narg('issue_prefix'), issue_prefix),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListWorkspacesWithRepos :many
-- Workspaces with a non-empty repo registry, to route a webhook to the repo's
-- owning workspace. ORDER BY id keeps the resolver's tie-break stable on replay.
SELECT id, repos FROM workspace
WHERE repos IS NOT NULL AND repos <> '[]'::jsonb
ORDER BY id;

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: DeleteWorkspace :exec
-- The channel_* tables carry no FK to workspace/chat_session (MUL-3515 §4), so
-- none of them cascade-delete with the workspace. Delete them explicitly here.
-- channel_installation must go or a deleted workspace's rows keep squatting the
-- (channel_type, app_id) routing index — permanently blocking the same IM bot
-- from being reconnected in any other workspace; the child rows (bindings,
-- tokens, dedup, session bindings, outbound cards) must go with it or they
-- linger forever as unreachable orphans. Rows without a workspace_id are reached
-- via their installation_id / chat_session_id (both scoped to this workspace);
-- all data-modifying CTEs run against one snapshot, so those subqueries still see
-- the channel_installation / chat_session rows this statement also removes.
WITH deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
), deleted_channel_user_bindings AS (
    DELETE FROM channel_user_binding WHERE workspace_id = $1
), deleted_channel_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE workspace_id = $1
), deleted_channel_chat_session_bindings AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id = $1)
), deleted_channel_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id = $1)
), deleted_channel_inbound_audit AS (
    DELETE FROM channel_inbound_audit
    WHERE installation_id IN (SELECT id FROM channel_installation WHERE workspace_id = $1)
), deleted_channel_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)
), deleted_channel_installations AS (
    DELETE FROM channel_installation WHERE workspace_id = $1
)
DELETE FROM workspace WHERE workspace.id = $1;
