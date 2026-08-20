-- DingTalk-specific installation identity operations. The underlying channel_*
-- tables are shared, but these replacement semantics belong to DingTalk's BYO
-- AppKey model and deliberately stay out of the shared channel query surface.

-- name: ListDingTalkUserBindingsForMember :many
-- Returns only the requesting Multica member's DingTalk identities. The
-- installation list is member-visible, so returning every member's staff id
-- here would expose staff ID values more broadly than necessary.
SELECT installation_id, channel_user_id
FROM channel_user_binding
WHERE workspace_id = sqlc.arg(workspace_id)
  AND multica_user_id = sqlc.arg(multica_user_id)
  AND channel_type = 'dingtalk'
ORDER BY bound_at DESC, id ASC;

-- name: DiscoverDingTalkGroupRoute :one
-- Persist a group only after the shared inbound router has accepted the @bot
-- message and validated the sender's identity/workspace membership. The INSERT
-- default is the installation's agent; a later admin reassignment survives
-- subsequent inbound events because the conflict update refreshes only the
-- human-readable title. The returned active bit revalidates the effective route
-- target at runtime while preserving the route across archive/restore. The
-- workspace/installation locks serialize discovery with teardown and revoke:
-- revoke-first rechecks status after the wait and returns no row; discovery-
-- first completes before revoke, matching the existing in-flight semantics.
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.id = sqlc.arg(workspace_id)
    FOR KEY SHARE
), installation AS MATERIALIZED (
    SELECT i.*
    FROM channel_installation i
    JOIN workspace_guard w ON w.id = i.workspace_id
    WHERE i.id = sqlc.arg(installation_id)
      AND i.workspace_id = sqlc.arg(workspace_id)
      AND i.channel_type = 'dingtalk'
      AND i.status = 'active'
    FOR SHARE OF i
), group_route AS (
    INSERT INTO dingtalk_group_route (
        workspace_id, installation_id, conversation_id,
        conversation_title, agent_id
    )
    SELECT
        i.workspace_id, i.id, sqlc.arg(conversation_id)::text,
        sqlc.arg(conversation_title)::text, i.agent_id
    FROM installation i
    ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
        conversation_title = CASE
            WHEN EXCLUDED.conversation_title = '' THEN dingtalk_group_route.conversation_title
            ELSE EXCLUDED.conversation_title
        END,
        updated_at = CASE
            WHEN EXCLUDED.conversation_title <> ''
             AND EXCLUDED.conversation_title IS DISTINCT FROM dingtalk_group_route.conversation_title
                THEN now()
            ELSE dingtalk_group_route.updated_at
        END
    RETURNING agent_id, workspace_id, revision
)
SELECT r.agent_id,
       r.revision,
       EXISTS (
           SELECT 1 FROM agent a
           WHERE a.id = r.agent_id
             AND a.workspace_id = r.workspace_id
             AND a.kind = 'user'
             AND a.archived_at IS NULL
       ) AS agent_active
FROM group_route r;

-- name: ListDingTalkGroupRoutesByWorkspace :many
SELECT r.*
FROM dingtalk_group_route r
JOIN channel_installation i ON i.id = r.installation_id
WHERE r.workspace_id = sqlc.arg(workspace_id)
  AND i.workspace_id = sqlc.arg(workspace_id)
  AND i.channel_type = 'dingtalk'
  AND i.status = 'active'
ORDER BY r.discovered_at ASC;

-- name: GetDingTalkGroupRouteInWorkspace :one
-- Handler diagnosis deliberately uses the same active-installation boundary as
-- reassignment. Retained routes stay available for reconnect internally, but a
-- revoked installation's route is not a PATCH-visible resource.
SELECT r.*
FROM dingtalk_group_route r
JOIN channel_installation i ON i.id = r.installation_id
WHERE r.id = sqlc.arg(id)
  AND r.workspace_id = sqlc.arg(workspace_id)
  AND i.workspace_id = sqlc.arg(workspace_id)
  AND i.channel_type = 'dingtalk'
  AND i.status = 'active';

-- name: ReassignDingTalkGroupRoute :one
-- Reassigning a group must also sever its existing chat-session binding. The
-- next message creates a fresh session for the new agent, so transcripts and
-- pending outbound updates cannot cross agent boundaries. The old chat session
-- remains as history; only its DingTalk route and outbound card state are
-- removed. The target agent existence/workspace/archive check is repeated here
-- so a concurrent archive or delete cannot create a dangling route. The active
-- workspace, active installation, and route locks preserve the teardown order;
-- the installation is share-locked before the route is update-locked. Revoke
-- takes an exclusive lock on that same installation row, defining the race:
-- PATCH-first may finish before revoke, while revoke-first makes PATCH wait,
-- re-check status, and return no row without touching the route or binding.
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.id = sqlc.arg(workspace_id)
    FOR KEY SHARE
), target_agent AS MATERIALIZED (
    SELECT a.id
    FROM agent a
    JOIN workspace_guard w ON w.id = a.workspace_id
    WHERE a.id = sqlc.arg(agent_id)
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE
), active_installation AS MATERIALIZED (
    SELECT i.id
    FROM channel_installation i
    JOIN dingtalk_group_route r ON r.installation_id = i.id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
      AND i.workspace_id = sqlc.arg(workspace_id)
      AND i.channel_type = 'dingtalk'
      AND i.status = 'active'
      AND EXISTS (SELECT 1 FROM target_agent)
    FOR SHARE OF i
), target AS (
    SELECT r.*, r.agent_id AS previous_agent_id
    FROM dingtalk_group_route r
    JOIN active_installation i ON i.id = r.installation_id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
    FOR UPDATE OF r
), updated AS (
    UPDATE dingtalk_group_route r
    SET agent_id = sqlc.arg(agent_id),
        revision = r.revision + 1,
        updated_at = now()
    FROM target t
    WHERE r.id = t.id
    RETURNING r.*, t.previous_agent_id
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding b
    USING updated u
    WHERE u.previous_agent_id IS DISTINCT FROM u.agent_id
      AND b.installation_id = u.installation_id
      AND b.channel_chat_id = u.conversation_id
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
)
SELECT id, workspace_id, installation_id, conversation_id,
       conversation_title, agent_id, revision, discovered_at, updated_at
FROM updated;

-- name: DingTalkGroupRouteMatchesAgent :one
SELECT EXISTS (
    SELECT 1
    FROM dingtalk_group_route r
    JOIN agent a ON a.id = r.agent_id
                AND a.workspace_id = r.workspace_id
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND r.agent_id = sqlc.arg(agent_id)
      AND r.revision = sqlc.arg(route_revision)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
) AS matches;

-- name: LockDingTalkGroupRouteForAppend :one
-- This is the durable cutover fence. The active target agent and route are
-- locked in the same order as ReassignDingTalkGroupRoute, and the lock remains
-- held by AppendUserMessage's transaction until the message and dedup mark
-- commit. A PATCH that wins first changes revision and produces no row; an
-- inbound append that wins first makes PATCH wait until that append commits.
WITH target_agent AS MATERIALIZED (
    SELECT a.id
    FROM agent a
    WHERE a.id = sqlc.arg(agent_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE
)
SELECT r.revision
FROM dingtalk_group_route r
WHERE r.installation_id = sqlc.arg(installation_id)
  AND r.conversation_id = sqlc.arg(conversation_id)::text
  AND r.agent_id = sqlc.arg(agent_id)
  AND r.revision = sqlc.arg(route_revision)
  AND EXISTS (SELECT 1 FROM target_agent)
FOR SHARE OF r;

-- name: DeleteDingTalkStaleGroupChatBinding :one
-- A route reassignment normally removes the group's binding in the same query.
-- An inbound message that resolved immediately before the reassignment can,
-- however, finish creating its old-agent binding immediately afterwards. Every
-- subsequent group message runs this guard before EnsureSession. Legacy
-- bindings have no agent stamp; preserve them when the effective route is still
-- the installation's original/default agent, but retire them after a genuine
-- reassignment so the new route can never inherit the old agent's transcript.
WITH cleared AS (
    DELETE FROM channel_chat_session_binding b
    WHERE b.installation_id = sqlc.arg(installation_id)
      AND b.channel_type = 'dingtalk'
      AND b.channel_chat_id = sqlc.arg(conversation_id)::text
      AND COALESCE(
          NULLIF(b.config ->> 'agent_id', ''),
          (
              SELECT i.agent_id::text
              FROM channel_installation i
              WHERE i.id = b.installation_id
          ),
          ''
      ) <> sqlc.arg(agent_id)::uuid::text
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared)
)
SELECT count(*)::bigint AS cleared_count
FROM cleared;

-- name: LockDingTalkInstallationOwner :exec
-- Serializes install / replacement decisions for one logical
-- (workspace, agent, channel) slot. A different-AppKey replacement deletes the
-- old row and inserts a fresh installation id; the advisory lock closes the
-- gap where two concurrent replacements could otherwise miss each other's new
-- row and update it in place, carrying identity state across robot boundaries.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        (sqlc.arg(workspace_id)::uuid)::text || ':' ||
        (sqlc.arg(agent_id)::uuid)::text || ':dingtalk',
        0
    )
);

-- name: GetDingTalkInstallationOwnerForUpdate :one
-- Reads the current robot identity after LockDingTalkInstallationOwner has
-- serialized the logical owner slot. app_id is non-null for every DingTalk
-- installation; COALESCE treats malformed legacy config as a different robot,
-- which safely replaces it instead of preserving unknown identity state.
SELECT id, COALESCE(config ->> 'app_id', '')::text AS app_id
FROM channel_installation
WHERE workspace_id = sqlc.arg(workspace_id)
  AND agent_id = sqlc.arg(agent_id)
  AND channel_type = 'dingtalk'
FOR UPDATE;

-- name: DeleteDingTalkInstallationForReplacement :one
-- Retires an installation when the same agent is connected with a DIFFERENT
-- AppKey. A senderStaffId is scoped to one DingTalk organization, so none of the
-- old installation's identity, token, session, dedup, or outbound state may
-- cross into the new robot. The caller inserts a fresh installation in the same
-- transaction, giving the replacement a new installation_id that also fences
-- late writes and replies from the old connection.
--
-- Chat sessions themselves remain as history, but their channel bindings and
-- outbound cards are removed. Audit and media-intent rows remain useful for
-- diagnostics / reconciliation, so their installation references are detached.
WITH retired AS (
    DELETE FROM channel_installation ci
    WHERE ci.id = sqlc.arg(installation_id)
      AND ci.workspace_id = sqlc.arg(workspace_id)
      AND ci.agent_id = sqlc.arg(agent_id)
      AND ci.channel_type = 'dingtalk'
    RETURNING ci.id
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM retired)
    RETURNING chat_session_id
),
cleared_group_routes AS (
    DELETE FROM dingtalk_group_route
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
),
cleared_binding_tokens AS (
    DELETE FROM channel_binding_token
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_user_bindings AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM retired)
),
detached_audit AS (
    UPDATE channel_inbound_audit SET installation_id = NULL
    WHERE installation_id IN (SELECT id FROM retired)
),
detached_media_intents AS (
    UPDATE channel_media_pending_object SET installation_id = NULL
    WHERE installation_id IN (SELECT id FROM retired)
)
SELECT retired.id FROM retired;
