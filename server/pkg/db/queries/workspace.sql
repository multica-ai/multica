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
-- Most of the workspace's rows cascade away through their FK to workspace(id).
-- The channel_* tables deliberately have NO FK (MUL-3515 §4), so their rows do
-- NOT cascade — a plain workspace delete would leave each channel_installation
-- behind, permanently holding the (channel_type, app_id) unique slot, so the IM
-- bot could never be reconnected to any agent (#4810 / MUL-3937). Delete this
-- workspace's channel installations and their app-layer dependents explicitly,
-- in the same atomic statement as the workspace delete. github_pending_check_suite
-- is likewise cleaned here because it has no workspace FK cascade.
WITH deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
),
target_channel_installs AS MATERIALIZED (
    SELECT channel_installation.id FROM channel_installation WHERE workspace_id = $1
),
_channel_sess AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM target_channel_installs)
),
_channel_tok AS (
    DELETE FROM channel_binding_token
    WHERE installation_id IN (SELECT id FROM target_channel_installs)
),
_channel_usr AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM target_channel_installs)
),
_channel_aud AS (
    UPDATE channel_inbound_audit SET installation_id = NULL
    WHERE installation_id IN (SELECT id FROM target_channel_installs)
),
_channel_installs AS (
    DELETE FROM channel_installation WHERE workspace_id = $1
)
DELETE FROM workspace WHERE workspace.id = $1;
