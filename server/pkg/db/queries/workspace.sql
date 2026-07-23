-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.settings,
       w.created_at, w.updated_at, w.context, w.repos,
       w.issue_prefix, w.issue_counter, w.avatar_url, w.attribution_fail_closed
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

-- name: GetWorkspaceAttributionFailClosed :one
-- Lean read of the fail-closed attribution policy for the enqueue hot path
-- (MUL-4302 §3.5), avoiding a full workspace-row fetch.
SELECT attribution_fail_closed FROM workspace
WHERE id = $1;

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

-- name: LockWorkspaceForDelete :one
-- Taken first by DeleteWorkspace, before it enumerates the workspace's chat
-- sessions. LockChatSessionsByWorkspace only covers sessions that exist when it
-- runs; a CreateChatSession committing during the delete window would add one
-- the lock set never saw, and a finalizer could then insert a restore for it
-- after the sweep's snapshot — orphaning the prompt (#5219).
--
-- The delete window is held closed against new sessions by an EXPLICIT protocol,
-- not the chat_session.workspace_id FK: every session creator takes
-- LockWorkspaceForChatSessionCreate (FOR KEY SHARE) on this row first, and this
-- FOR UPDATE conflicts with it. Keeping the bar in the app layer means it does
-- not silently break if that FK is ever dropped (the codebase is moving FK
-- relationships into the application layer, MUL-3515). Lock order is
-- workspace -> chat_session -> agent_task_queue; the finalizer never touches
-- workspace, so this cannot deadlock against it.
SELECT id FROM workspace WHERE id = $1 FOR UPDATE;

-- name: LockWorkspaceForChatSessionCreate :one
-- The creator half of the workspace delete/create protocol (#5219). Every
-- production path that inserts a chat_session takes this FOR KEY SHARE lock on the
-- parent workspace row, inside its transaction, before CreateChatSession. It
-- conflicts with DeleteWorkspace's FOR UPDATE (so a create is blocked while a
-- delete is in progress, and vice versa) but not with other creators (FOR KEY
-- SHARE locks share), so concurrent session creation stays unserialized. This
-- makes the mutual exclusion explicit rather than leaning on the workspace FK's
-- implicit FOR KEY SHARE, which would vanish if that FK is dropped.
SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE;

-- name: DeleteWorkspace :exec
-- The channel_* tables (MUL-3515 §4), resource-label junctions, and custom issue
-- property definitions carry NO FK to workspace, so — unlike the CASCADE-backed
-- tables the DELETE below sweeps — they are not cleaned up implicitly. Remove
-- their workspace-owned rows here so they commit or roll back atomically with
-- the workspace row.
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
cleared_draft_restores AS (
    -- chat_draft_restore is keyed by chat_session_id with no FK (MUL-3515) and has
    -- no reaper, while its chat_session rows cascade away with the workspace. Reach
    -- them directly through chat_session (unlike the cards above, this is not
    -- limited to channel-bound sessions) or every pending restore — each holding a
    -- user's prompt text — would outlive the workspace permanently (#5219).
    --
    -- This sweep only sees restores committed before the statement's snapshot, so
    -- the caller must already hold LockChatSessionsByWorkspace: that lock is what
    -- keeps FinalizeDeferredCancelledChat from inserting one behind it.
    DELETE FROM chat_draft_restore
    WHERE chat_session_id IN (SELECT id FROM chat_session WHERE workspace_id = $1)
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
cleared_issue_properties AS (
    DELETE FROM issue_property WHERE workspace_id = $1
),
deleted_pending_check_suites AS (
    DELETE FROM github_pending_check_suite WHERE workspace_id = $1
),
cleared_client_usage_workspace AS (
    UPDATE client_usage_daily SET workspace_id = NULL WHERE workspace_id = $1
)
DELETE FROM workspace WHERE workspace.id = $1;
