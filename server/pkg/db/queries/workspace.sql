-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.issue_prefix, w.issue_counter, w.avatar_url
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.created_at ASC;

-- name: ListDaemonWorkspaces :many
-- Daemons only need the membership set and display name to discover which
-- workspaces should have local runtimes. Keep this projection intentionally
-- narrow so the periodic consistency check never reads UI-only JSON/text
-- columns such as settings, repos, or context.
SELECT w.id, w.name
FROM member m
JOIN workspace w ON w.id = m.workspace_id
WHERE m.user_id = $1
ORDER BY w.id ASC;

-- name: GetDaemonWorkspace :one
-- Workspace-scoped daemon tokens do not carry a user ID. This narrow lookup
-- lets them use the same endpoint without widening their token scope.
SELECT id, name
FROM workspace
WHERE id = $1;

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

-- name: IncrementIssueCounter :one
UPDATE workspace SET issue_counter = issue_counter + 1
WHERE id = $1
RETURNING issue_counter;

-- name: DeleteWorkspace :exec
-- The channel_* tables (MUL-3515 §4) and resource-label junctions carry NO FK to
-- workspace, so — unlike the CASCADE-backed tables the DELETE below sweeps —
-- they are not cleaned up implicitly. Remove their workspace-owned rows here so
-- they commit or roll back atomically with the workspace row.
WITH ws_installations AS (
    SELECT id FROM channel_installation WHERE workspace_id = $1
),
ws_agents AS (
    SELECT id FROM agent WHERE workspace_id = $1
),
ws_skills AS (
    SELECT id FROM skill WHERE workspace_id = $1
),
cleared_agent_label_assignments AS (
    DELETE FROM agent_to_label WHERE agent_id IN (SELECT id FROM ws_agents)
),
cleared_skill_label_assignments AS (
    DELETE FROM skill_to_label WHERE skill_id IN (SELECT id FROM ws_skills)
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding WHERE installation_id IN (SELECT id FROM ws_installations)
    RETURNING chat_session_id
),
cleared_outbound_cards AS (
    -- channel_outbound_card_message is keyed by chat_session_id (no FK); its own
    -- chat_session rows cascade away with the workspace, so reach the cards through
    -- the just-removed chat-session bindings, which still carry the id.
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_audit AS (
    -- Purge, don't detach: the workspace is gone and channel_inbound_audit has no
    -- workspace_id and no reaper, so a detached (NULL) row would be permanently
    -- unattributable. (Reclaim, where the workspace survives, still detaches.)
    DELETE FROM channel_inbound_audit WHERE installation_id IN (SELECT id FROM ws_installations)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding WHERE workspace_id = $1
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token WHERE workspace_id = $1
),
cleared_installations AS (
    DELETE FROM channel_installation WHERE workspace_id = $1
),
deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
)
DELETE FROM workspace WHERE workspace.id = $1;
