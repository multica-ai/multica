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

-- name: UpsertDingTalkBotIdentity :one
-- Bot identity and the optional permission issue belong to the installation,
-- not to any one group. Keep this as a separate command so the mixed-version
-- compatibility trigger can observe the completed command state when the
-- group-presence command runs next.
WITH target AS MATERIALIZED (
    SELECT channel_installation.id, channel_installation.workspace_id
    FROM channel_installation
    WHERE channel_installation.id = sqlc.arg(installation_id)
      AND channel_installation.workspace_id = sqlc.arg(workspace_id)
      AND channel_installation.channel_type = 'dingtalk'
      AND channel_installation.status = 'active'
    FOR UPDATE
), prior_identity AS (
    SELECT
        COALESCE(identity.bot_name, '')::text AS bot_name,
        COALESCE(identity.bot_identity_issue, '')::text AS bot_identity_issue
    FROM target
    LEFT JOIN dingtalk_bot_identity identity ON identity.installation_id = target.id
), upserted_identity AS (
    INSERT INTO dingtalk_bot_identity (
        workspace_id, installation_id, bot_name, bot_identity_issue
    )
    SELECT
        target.workspace_id,
        target.id,
        COALESCE(NULLIF(sqlc.arg(bot_name)::text, ''), prior_identity.bot_name),
        CASE
            WHEN sqlc.arg(bot_identity_issue)::text <> '' THEN sqlc.arg(bot_identity_issue)::text
            WHEN sqlc.arg(bot_name)::text <> '' THEN ''
            ELSE prior_identity.bot_identity_issue
        END
    FROM target, prior_identity
    ON CONFLICT (installation_id) DO UPDATE SET
        workspace_id = EXCLUDED.workspace_id,
        bot_name = EXCLUDED.bot_name,
        bot_identity_issue = EXCLUDED.bot_identity_issue,
        updated_at = now()
    WHERE dingtalk_bot_identity.workspace_id IS DISTINCT FROM EXCLUDED.workspace_id
       OR dingtalk_bot_identity.bot_name IS DISTINCT FROM EXCLUDED.bot_name
       OR dingtalk_bot_identity.bot_identity_issue IS DISTINCT FROM EXCLUDED.bot_identity_issue
    RETURNING installation_id
)
SELECT target.id FROM target;

-- name: RecordDingTalkGroupPresence :one
-- Records an observed bot/group pair without carrying routing state. The
-- installation identity is copied only for compatibility with the preceding
-- draft; readers use dingtalk_bot_identity as the authority.
WITH target AS MATERIALIZED (
    SELECT channel_installation.id, channel_installation.workspace_id
    FROM channel_installation
    WHERE channel_installation.id = sqlc.arg(installation_id)
      AND channel_installation.workspace_id = sqlc.arg(workspace_id)
      AND channel_installation.channel_type = 'dingtalk'
      AND channel_installation.status = 'active'
    FOR KEY SHARE
), current_identity AS (
    SELECT identity.installation_id, identity.bot_name, identity.bot_identity_issue
    FROM target
    JOIN dingtalk_bot_identity identity ON identity.installation_id = target.id
), observed AS (
    INSERT INTO dingtalk_group_presence (
        workspace_id, installation_id, conversation_id, conversation_title,
        bot_name, bot_identity_issue
    )
    SELECT
        target.workspace_id,
        target.id,
        sqlc.arg(conversation_id)::text,
        sqlc.arg(conversation_title)::text,
        current_identity.bot_name,
        current_identity.bot_identity_issue
    FROM target
    JOIN current_identity ON current_identity.installation_id = target.id
    ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
        conversation_title = CASE
            WHEN EXCLUDED.conversation_title <> '' THEN EXCLUDED.conversation_title
            ELSE dingtalk_group_presence.conversation_title
        END,
        bot_name = EXCLUDED.bot_name,
        bot_identity_issue = EXCLUDED.bot_identity_issue,
        updated_at = now()
    WHERE (
        dingtalk_group_presence.conversation_title IS DISTINCT FROM EXCLUDED.conversation_title
        AND EXCLUDED.conversation_title <> ''
    )
       OR dingtalk_group_presence.bot_name IS DISTINCT FROM EXCLUDED.bot_name
       OR dingtalk_group_presence.bot_identity_issue IS DISTINCT FROM EXCLUDED.bot_identity_issue
    RETURNING installation_id
), synced_identity AS (
    UPDATE dingtalk_group_presence presence
    SET
        bot_name = current_identity.bot_name,
        bot_identity_issue = current_identity.bot_identity_issue,
        updated_at = now()
    FROM current_identity
    WHERE presence.installation_id = current_identity.installation_id
      AND presence.conversation_id <> sqlc.arg(conversation_id)::text
      AND (
          presence.bot_name IS DISTINCT FROM current_identity.bot_name
          OR presence.bot_identity_issue IS DISTINCT FROM current_identity.bot_identity_issue
      )
)
SELECT target.id FROM target;

-- name: RecordDingTalkGroupActivity :one
-- Advances activity only after the deduplicated inbound message is durable.
-- Presence observation runs first, so a forgotten group is recreated before
-- this update executes.
UPDATE dingtalk_group_presence
SET last_active_at = now(), mention_count = mention_count + 1, updated_at = now()
WHERE installation_id = sqlc.arg(installation_id)
  AND conversation_id = sqlc.arg(conversation_id)
RETURNING installation_id;

-- name: ListDingTalkGroupPresencesByWorkspace :many
-- Active rows are the default inventory. Inactive rows are loaded only when a
-- client expands one installation's historical section.
SELECT
    presence.conversation_id,
    presence.conversation_title,
    presence.installation_id,
    installation.agent_id,
    COALESCE(identity.bot_name, presence.bot_name)::text AS bot_name,
    COALESCE(identity.bot_identity_issue, presence.bot_identity_issue)::text AS bot_identity_issue,
    presence.last_active_at,
    presence.mention_count
FROM dingtalk_group_presence presence
JOIN channel_installation installation ON installation.id = presence.installation_id
LEFT JOIN dingtalk_bot_identity identity ON identity.installation_id = presence.installation_id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (NOT sqlc.arg(filter_by_agent)::boolean OR installation.agent_id = sqlc.arg(agent_id))
  AND (NOT sqlc.arg(filter_by_installation)::boolean OR installation.id = sqlc.arg(filter_installation_id))
  AND (
      (NOT sqlc.arg(include_inactive)::boolean AND presence.last_active_at >= sqlc.arg(active_since))
      OR (sqlc.arg(include_inactive)::boolean AND (presence.last_active_at < sqlc.arg(active_since) OR presence.last_active_at IS NULL))
  )
ORDER BY presence.last_active_at DESC NULLS LAST, presence.conversation_title ASC,
         presence.conversation_id ASC, installation.installed_at ASC, installation.id ASC
LIMIT NULLIF(sqlc.arg(page_limit)::integer, 0)
OFFSET sqlc.arg(page_offset)::integer;

-- name: CountInactiveDingTalkGroupPresencesByWorkspace :many
SELECT presence.installation_id, installation.agent_id, count(*)::bigint AS group_count
FROM dingtalk_group_presence presence
JOIN channel_installation installation ON installation.id = presence.installation_id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (presence.last_active_at < sqlc.arg(active_since) OR presence.last_active_at IS NULL)
  AND (NOT sqlc.arg(filter_by_agent)::boolean OR installation.agent_id = sqlc.arg(agent_id))
GROUP BY presence.installation_id, installation.agent_id;

-- name: ListDingTalkBotIdentitiesByWorkspace :many
-- Identity is installation-owned, so forgetting or collapsing groups cannot
-- change the connection label.
SELECT
    installation.id AS installation_id,
    installation.agent_id,
    identity.bot_name,
    identity.bot_identity_issue
FROM channel_installation installation
JOIN dingtalk_bot_identity identity ON identity.installation_id = installation.id
WHERE installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
  AND installation.status = 'active'
  AND (NOT sqlc.arg(filter_by_agent)::boolean OR installation.agent_id = sqlc.arg(agent_id));

-- name: ForgetDingTalkGroupPresence :one
DELETE FROM dingtalk_group_presence presence
USING channel_installation installation
WHERE presence.installation_id = installation.id
  AND presence.workspace_id = sqlc.arg(workspace_id)
  AND presence.installation_id = sqlc.arg(installation_id)
  AND presence.conversation_id = sqlc.arg(conversation_id)
  AND installation.workspace_id = sqlc.arg(workspace_id)
  AND installation.channel_type = 'dingtalk'
RETURNING presence.installation_id;

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
cleared_group_presence AS (
    DELETE FROM dingtalk_group_presence
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_bot_identity AS (
    DELETE FROM dingtalk_bot_identity
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_group_routes AS (
    DELETE FROM dingtalk_group_route
    WHERE installation_id IN (SELECT id FROM retired)
),
cleared_chat_sessions AS (
    DELETE FROM channel_chat_session_binding
    WHERE installation_id IN (SELECT id FROM retired)
    RETURNING chat_session_id
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
