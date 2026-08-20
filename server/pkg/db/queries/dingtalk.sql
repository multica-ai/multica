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
-- message and validated workspace membership. A newly discovered group inherits
-- the installation's Agent or Squad target. Later reassignments survive because
-- the conflict update refreshes only the title. AgentID is resolved at read time:
-- for a Squad it is the current active Leader, never a fan-out target.
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
        conversation_title, agent_id, target_type, target_id
    )
    SELECT
        i.workspace_id, i.id, sqlc.arg(conversation_id)::text,
        sqlc.arg(conversation_title)::text,
        COALESCE(i.target_id, i.agent_id),
        COALESCE(i.target_type, 'agent'),
        COALESCE(i.target_id, i.agent_id)
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
    RETURNING workspace_id, target_type, target_id, revision
), target_agent AS MATERIALIZED (
    SELECT a.id,
           (a.kind = 'user' AND a.archived_at IS NULL) AS active
    FROM group_route r
    JOIN agent a ON a.id = r.target_id AND a.workspace_id = r.workspace_id
    WHERE r.target_type = 'agent'
    FOR SHARE OF a
), target_squad AS MATERIALIZED (
    SELECT s.leader_id AS id,
           s.leader_revision,
           (s.archived_at IS NULL
            AND a.kind = 'user'
            AND a.archived_at IS NULL) AS active
    FROM group_route r
    JOIN squad s ON s.id = r.target_id AND s.workspace_id = r.workspace_id
    JOIN agent a ON a.id = s.leader_id AND a.workspace_id = s.workspace_id
    WHERE r.target_type = 'squad'
    FOR SHARE OF s, a
)
SELECT r.target_type,
       r.target_id,
       COALESCE((SELECT id FROM target_agent), (SELECT id FROM target_squad))::uuid AS agent_id,
       (CASE WHEN r.target_type = 'squad'
             THEN COALESCE((SELECT leader_revision FROM target_squad), 0)
             ELSE 0 END)::bigint AS target_revision,
       r.revision,
       COALESCE(
           CASE WHEN r.target_type = 'squad'
                THEN (SELECT active FROM target_squad)
                ELSE (SELECT active FROM target_agent)
           END,
           false
       )::boolean AS agent_active
FROM group_route r;

-- name: ListDingTalkGroupRoutesByWorkspace :many
SELECT r.id, r.workspace_id, r.installation_id, r.conversation_id,
       r.conversation_title, r.target_type,
       COALESCE(r.target_id, r.agent_id) AS target_id,
       (CASE WHEN r.target_type = 'squad' THEN s.leader_id
             ELSE COALESCE(r.target_id, r.agent_id) END)::uuid AS agent_id,
       r.revision, r.discovered_at, r.updated_at
FROM dingtalk_group_route r
JOIN channel_installation i ON i.id = r.installation_id
LEFT JOIN squad s ON r.target_type = 'squad'
                 AND s.id = r.target_id
                 AND s.workspace_id = r.workspace_id
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
-- Validate and lock the selected Agent or Squad Leader, then atomically replace
-- the product target and sever the previous chat binding. The next message must
-- create a fresh session, so transcripts and late replies cannot cross targets.
WITH workspace_guard AS MATERIALIZED (
    SELECT w.id
    FROM workspace w
    WHERE w.id = sqlc.arg(workspace_id)
    FOR KEY SHARE
), target_agent AS MATERIALIZED (
    SELECT a.id AS agent_id
    FROM agent a
    JOIN workspace_guard w ON w.id = a.workspace_id
    WHERE sqlc.arg(target_type)::text = 'agent'
      AND a.id = sqlc.arg(target_id)
      AND a.workspace_id = sqlc.arg(workspace_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), target_squad AS MATERIALIZED (
    SELECT s.leader_id AS agent_id
    FROM squad s
    JOIN workspace_guard w ON w.id = s.workspace_id
    JOIN agent a ON a.id = s.leader_id AND a.workspace_id = s.workspace_id
    WHERE sqlc.arg(target_type)::text = 'squad'
      AND s.id = sqlc.arg(target_id)
      AND s.workspace_id = sqlc.arg(workspace_id)
      AND s.archived_at IS NULL
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF s, a
), effective_target AS MATERIALIZED (
    SELECT agent_id FROM target_agent
    UNION ALL
    SELECT agent_id FROM target_squad
), active_installation AS MATERIALIZED (
    SELECT i.id
    FROM channel_installation i
    JOIN dingtalk_group_route r ON r.installation_id = i.id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
      AND i.workspace_id = sqlc.arg(workspace_id)
      AND i.channel_type = 'dingtalk'
      AND i.status = 'active'
      AND EXISTS (SELECT 1 FROM effective_target)
    FOR SHARE OF i
), target AS (
    SELECT r.*,
           r.target_type AS previous_target_type,
           COALESCE(r.target_id, r.agent_id) AS previous_target_id
    FROM dingtalk_group_route r
    JOIN active_installation i ON i.id = r.installation_id
    WHERE r.id = sqlc.arg(id)
      AND r.workspace_id = sqlc.arg(workspace_id)
    FOR UPDATE OF r
), updated AS (
    UPDATE dingtalk_group_route r
    SET agent_id = sqlc.arg(target_id),
        target_type = sqlc.arg(target_type)::text,
        target_id = sqlc.arg(target_id),
        revision = r.revision + 1,
        updated_at = now()
    FROM target t
    WHERE r.id = t.id
    RETURNING r.*, t.previous_target_type, t.previous_target_id
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding b
    USING updated u
    WHERE (u.previous_target_type, u.previous_target_id)
          IS DISTINCT FROM (u.target_type, u.target_id)
      AND b.installation_id = u.installation_id
      AND b.channel_chat_id = u.conversation_id
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
)
SELECT id, workspace_id, installation_id, conversation_id,
       conversation_title, target_type, target_id,
       (SELECT agent_id FROM effective_target) AS agent_id,
       revision, discovered_at, updated_at
FROM updated;

-- name: DingTalkGroupRouteMatchesTarget :one
SELECT EXISTS (
    SELECT 1
    FROM dingtalk_group_route r
    LEFT JOIN squad s ON r.target_type = 'squad'
                     AND s.id = r.target_id
                     AND s.workspace_id = r.workspace_id
    JOIN agent a ON a.id = CASE WHEN r.target_type = 'squad'
                                THEN s.leader_id ELSE r.target_id END
                AND a.workspace_id = r.workspace_id
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND r.target_type = sqlc.arg(target_type)::text
      AND r.target_id = sqlc.arg(target_id)
      AND a.id = sqlc.arg(agent_id)
      AND r.revision = sqlc.arg(route_revision)
      AND (r.target_type <> 'squad' OR (
          s.archived_at IS NULL
          AND s.leader_revision = sqlc.arg(target_revision)
      ))
      AND a.kind = 'user'
      AND a.archived_at IS NULL
) AS matches;

-- name: DingTalkInstallationMatchesTarget :one
SELECT EXISTS (
    SELECT 1
    FROM channel_installation ci
    LEFT JOIN squad s ON ci.target_type = 'squad'
                     AND s.id = ci.target_id
                     AND s.workspace_id = ci.workspace_id
    JOIN agent a ON a.id = CASE WHEN ci.target_type = 'squad'
                                THEN s.leader_id ELSE COALESCE(ci.target_id, ci.agent_id) END
                AND a.workspace_id = ci.workspace_id
    WHERE ci.id = sqlc.arg(installation_id)
      AND ci.workspace_id = sqlc.arg(workspace_id)
      AND ci.channel_type = 'dingtalk'
      AND ci.status = 'active'
      AND COALESCE(ci.target_type, 'agent') = sqlc.arg(target_type)::text
      AND COALESCE(ci.target_id, ci.agent_id) = sqlc.arg(target_id)
      AND a.id = sqlc.arg(agent_id)
      AND (ci.target_type <> 'squad' OR (
          s.archived_at IS NULL
          AND s.leader_revision = sqlc.arg(target_revision)
      ))
      AND a.kind = 'user'
      AND a.archived_at IS NULL
) AS matches;

-- name: LockDingTalkGroupRouteForAppend :one
-- This is the durable cutover fence. The active target agent and route are
-- locked in the same order as ReassignDingTalkGroupRoute, and the lock remains
-- held by AppendUserMessage's transaction until the message and dedup mark
-- commit. A PATCH that wins first changes revision and produces no row; an
-- inbound append that wins first makes PATCH wait until that append commits.
WITH route AS MATERIALIZED (
    SELECT r.*
    FROM dingtalk_group_route r
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND r.target_type = sqlc.arg(target_type)::text
      AND r.target_id = sqlc.arg(target_id)
      AND r.revision = sqlc.arg(route_revision)
    FOR SHARE OF r
), target_agent AS MATERIALIZED (
    SELECT a.id
    FROM route r
    JOIN agent a ON a.id = r.target_id AND a.workspace_id = r.workspace_id
    WHERE r.target_type = 'agent'
      AND a.id = sqlc.arg(agent_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), target_squad AS MATERIALIZED (
    SELECT a.id
    FROM route r
    JOIN squad s ON s.id = r.target_id AND s.workspace_id = r.workspace_id
    JOIN agent a ON a.id = s.leader_id AND a.workspace_id = s.workspace_id
    WHERE r.target_type = 'squad'
      AND a.id = sqlc.arg(agent_id)
      AND s.leader_revision = sqlc.arg(target_revision)
      AND s.archived_at IS NULL
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF s, a
)
SELECT r.revision
FROM route r
WHERE EXISTS (SELECT 1 FROM target_agent)
   OR EXISTS (SELECT 1 FROM target_squad);

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
      AND (
          COALESCE(NULLIF(b.config ->> 'target_type', ''), 'agent')
              <> sqlc.arg(target_type)::text
          OR COALESCE(
              NULLIF(b.config ->> 'target_id', ''),
              NULLIF(b.config ->> 'agent_id', ''),
              (SELECT COALESCE(i.target_id, i.agent_id)::text
               FROM channel_installation i WHERE i.id = b.installation_id),
              ''
          ) <> sqlc.arg(target_id)::uuid::text
          OR COALESCE(
              NULLIF(b.config ->> 'agent_id', ''),
              (SELECT i.agent_id::text
               FROM channel_installation i WHERE i.id = b.installation_id),
              ''
          ) <> sqlc.arg(agent_id)::uuid::text
          OR (
              sqlc.arg(target_type)::text = 'squad'
              AND COALESCE(NULLIF(b.config ->> 'target_revision', ''), '0')
                  <> sqlc.arg(target_revision)::bigint::text
          )
      )
    RETURNING b.chat_session_id
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared)
)
SELECT count(*)::bigint AS cleared_count
FROM cleared;

-- name: LockDingTalkInstallationOwner :exec
-- Serializes install / replacement decisions for one logical
-- (workspace, target, channel) slot. A different-AppKey replacement deletes the
-- old row and inserts a fresh installation id; the advisory lock closes the
-- gap where two concurrent replacements could otherwise miss each other's new
-- row and update it in place, carrying identity state across robot boundaries.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        (sqlc.arg(workspace_id)::uuid)::text || ':' ||
        sqlc.arg(target_type)::text || ':' ||
        (sqlc.arg(target_id)::uuid)::text || ':dingtalk',
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
  AND COALESCE(target_type, 'agent') = sqlc.arg(target_type)::text
  AND COALESCE(target_id, agent_id) = sqlc.arg(target_id)
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
      AND COALESCE(ci.target_type, 'agent') = sqlc.arg(target_type)::text
      AND COALESCE(ci.target_id, ci.agent_id) = sqlc.arg(target_id)
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

-- name: UpsertDingTalkTargetInstallation :one
-- DingTalk keeps the shared legacy agent_id column as the unique owner key.
-- Agent targets store the Agent id; Squad targets store the Squad id. Runtime
-- dispatch never executes that legacy value directly: the resolver below maps
-- target_type + target_id to the current Leader and returns it as AgentID.
INSERT INTO channel_installation (
    workspace_id, agent_id, target_type, target_id,
    channel_type, config, installer_user_id
) VALUES (
    sqlc.arg(workspace_id), sqlc.arg(target_id),
    sqlc.arg(target_type)::text, sqlc.arg(target_id),
    'dingtalk', sqlc.arg(config), sqlc.arg(installer_user_id)
)
ON CONFLICT (workspace_id, agent_id, channel_type) DO UPDATE SET
    target_type      = EXCLUDED.target_type,
    target_id        = EXCLUDED.target_id,
    config           = EXCLUDED.config,
    installer_user_id = EXCLUDED.installer_user_id,
    status           = 'active',
    installed_at     = now(),
    updated_at       = now()
RETURNING *;

-- name: ReclaimDeadDingTalkInstallationByAppID :one
-- Target-aware form of the shared dead-owner reclaim. A Squad installation is
-- live while its Squad exists, even though its legacy agent_id stores a Squad id
-- and therefore has no matching agent row.
WITH dead AS (
    DELETE FROM channel_installation ci
    WHERE ci.channel_type = 'dingtalk'
      AND ci.config ->> 'app_id' = sqlc.arg(app_id)::text
      AND (
            (ci.status = 'revoked' AND NOT (
                ci.workspace_id = sqlc.arg(workspace_id)
                AND COALESCE(ci.target_type, 'agent') = sqlc.arg(target_type)::text
                AND COALESCE(ci.target_id, ci.agent_id) = sqlc.arg(target_id)
            ))
         OR NOT EXISTS (SELECT 1 FROM workspace w WHERE w.id = ci.workspace_id)
         OR (
              COALESCE(ci.target_type, 'agent') = 'agent'
              AND NOT EXISTS (
                  SELECT 1 FROM agent a
                  WHERE a.id = COALESCE(ci.target_id, ci.agent_id)
                    AND a.workspace_id = ci.workspace_id
              )
         )
         OR (
              ci.target_type = 'squad'
              AND NOT EXISTS (
                  SELECT 1 FROM squad s
                  WHERE s.id = ci.target_id
                    AND s.workspace_id = ci.workspace_id
              )
         )
      )
    RETURNING ci.id
), cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM dead)
    RETURNING chat_session_id
), cleared_group_routes AS (
    DELETE FROM dingtalk_group_route
    WHERE installation_id IN (SELECT id FROM dead)
), cleared_outbound_cards AS (
    DELETE FROM channel_outbound_card_message
    WHERE chat_session_id IN (SELECT chat_session_id FROM cleared_chat_sessions)
), cleared_binding_tokens AS (
    DELETE FROM channel_binding_token
    WHERE installation_id IN (SELECT id FROM dead)
), cleared_user_bindings AS (
    DELETE FROM channel_user_binding
    WHERE installation_id IN (SELECT id FROM dead)
), cleared_inbound_dedup AS (
    DELETE FROM channel_inbound_message_dedup
    WHERE installation_id IN (SELECT id FROM dead)
), detached_audit AS (
    UPDATE channel_inbound_audit SET installation_id = NULL
    WHERE installation_id IN (SELECT id FROM dead)
)
SELECT id FROM dead;

-- name: GetDingTalkInstallationTargetOwnerByAppID :one
SELECT ci.workspace_id,
       COALESCE(ci.target_type, 'agent')::text AS target_type,
       COALESCE(ci.target_id, ci.agent_id) AS target_id,
       (CASE WHEN ci.target_type = 'squad' THEN s.archived_at
             ELSE a.archived_at END)::timestamptz AS target_archived_at
FROM channel_installation ci
LEFT JOIN agent a ON COALESCE(ci.target_type, 'agent') = 'agent'
                 AND a.id = COALESCE(ci.target_id, ci.agent_id)
                 AND a.workspace_id = ci.workspace_id
LEFT JOIN squad s ON ci.target_type = 'squad'
                 AND s.id = ci.target_id
                 AND s.workspace_id = ci.workspace_id
WHERE ci.channel_type = 'dingtalk'
  AND ci.config ->> 'app_id' = sqlc.arg(app_id)::text;

-- name: ListDingTalkInstallationsByWorkspace :many
SELECT ci.id, ci.workspace_id,
       COALESCE(ci.target_type, 'agent')::text AS target_type,
       COALESCE(ci.target_id, ci.agent_id) AS target_id,
       (CASE WHEN ci.target_type = 'squad' THEN s.leader_id
             ELSE COALESCE(ci.target_id, ci.agent_id) END)::uuid AS agent_id,
       ci.installer_user_id, ci.status, ci.config,
       ci.ws_lease_token, ci.ws_lease_expires_at,
       ci.installed_at, ci.created_at, ci.updated_at
FROM channel_installation ci
LEFT JOIN squad s ON ci.target_type = 'squad'
                 AND s.id = ci.target_id
                 AND s.workspace_id = ci.workspace_id
WHERE ci.workspace_id = sqlc.arg(workspace_id)
  AND ci.channel_type = 'dingtalk'
ORDER BY ci.created_at ASC;

-- name: ResolveDingTalkInstallationByAppID :one
SELECT ci.id, ci.workspace_id,
       COALESCE(ci.target_type, 'agent')::text AS target_type,
       COALESCE(ci.target_id, ci.agent_id) AS target_id,
       (CASE WHEN ci.target_type = 'squad' THEN s.leader_id
             ELSE COALESCE(ci.target_id, ci.agent_id) END)::uuid AS agent_id,
       ci.installer_user_id, ci.status, ci.config,
       ci.ws_lease_token, ci.ws_lease_expires_at,
       ci.installed_at, ci.created_at, ci.updated_at,
       (CASE WHEN ci.target_type = 'squad' THEN s.leader_revision ELSE 0 END)::bigint AS target_revision,
       (CASE WHEN ci.target_type = 'squad'
            THEN s.id IS NOT NULL AND s.archived_at IS NULL
                 AND a.id IS NOT NULL AND a.kind = 'user' AND a.archived_at IS NULL
            ELSE a.id IS NOT NULL AND a.kind = 'user' AND a.archived_at IS NULL
       END)::boolean AS target_active
FROM channel_installation ci
LEFT JOIN squad s ON ci.target_type = 'squad'
                 AND s.id = ci.target_id
                 AND s.workspace_id = ci.workspace_id
LEFT JOIN agent a ON a.id = CASE WHEN ci.target_type = 'squad'
                                 THEN s.leader_id ELSE COALESCE(ci.target_id, ci.agent_id) END
                 AND a.workspace_id = ci.workspace_id
WHERE ci.channel_type = 'dingtalk'
  AND ci.config ->> 'app_id' = sqlc.arg(app_id)::text;

-- name: LockDingTalkDirectOutboundTarget :one
WITH installation AS MATERIALIZED (
    SELECT ci.*
    FROM channel_installation ci
    WHERE ci.id = sqlc.arg(installation_id)
      AND ci.workspace_id = sqlc.arg(workspace_id)
      AND ci.channel_type = 'dingtalk'
      AND ci.status = 'active'
      AND COALESCE(ci.target_type, 'agent') = sqlc.arg(target_type)::text
      AND COALESCE(ci.target_id, ci.agent_id) = sqlc.arg(target_id)
    FOR SHARE OF ci
), target_agent AS MATERIALIZED (
    SELECT a.id
    FROM installation i
    JOIN agent a ON a.id = COALESCE(i.target_id, i.agent_id)
                AND a.workspace_id = i.workspace_id
    WHERE COALESCE(i.target_type, 'agent') = 'agent'
      AND a.id = sqlc.arg(agent_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), target_squad AS MATERIALIZED (
    SELECT a.id
    FROM installation i
    JOIN squad s ON s.id = i.target_id AND s.workspace_id = i.workspace_id
    JOIN agent a ON a.id = s.leader_id AND a.workspace_id = s.workspace_id
    WHERE i.target_type = 'squad'
      AND a.id = sqlc.arg(agent_id)
      AND s.leader_revision = sqlc.arg(target_revision)
      AND s.archived_at IS NULL
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF s, a
)
SELECT id FROM target_agent
UNION ALL
SELECT id FROM target_squad;

-- name: LockDingTalkGroupOutboundTarget :one
WITH route AS MATERIALIZED (
    SELECT r.*
    FROM dingtalk_group_route r
    JOIN channel_installation ci ON ci.id = r.installation_id
    WHERE r.installation_id = sqlc.arg(installation_id)
      AND r.workspace_id = sqlc.arg(workspace_id)
      AND r.conversation_id = sqlc.arg(conversation_id)::text
      AND r.target_type = sqlc.arg(target_type)::text
      AND r.target_id = sqlc.arg(target_id)
      AND ci.status = 'active'
      AND ci.channel_type = 'dingtalk'
    FOR SHARE OF r, ci
), target_agent AS MATERIALIZED (
    SELECT a.id
    FROM route r
    JOIN agent a ON a.id = r.target_id AND a.workspace_id = r.workspace_id
    WHERE r.target_type = 'agent'
      AND a.id = sqlc.arg(agent_id)
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF a
), target_squad AS MATERIALIZED (
    SELECT a.id
    FROM route r
    JOIN squad s ON s.id = r.target_id AND s.workspace_id = r.workspace_id
    JOIN agent a ON a.id = s.leader_id AND a.workspace_id = s.workspace_id
    WHERE r.target_type = 'squad'
      AND a.id = sqlc.arg(agent_id)
      AND s.leader_revision = sqlc.arg(target_revision)
      AND s.archived_at IS NULL
      AND a.kind = 'user'
      AND a.archived_at IS NULL
    FOR SHARE OF s, a
)
SELECT id FROM target_agent
UNION ALL
SELECT id FROM target_squad;
