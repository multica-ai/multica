-- ShareCRM-specific installation identity operations. The underlying channel_*
-- tables are shared, but these replacement semantics belong to ShareCRM's BYO
-- App ID model and deliberately stay out of the shared channel query surface.
-- Pattern mirrors dingtalk.sql (MUL-3958 AppKey identity boundary).

-- name: LockShareCRMInstallationOwner :exec
-- Serializes install / replacement decisions for one logical
-- (workspace, agent, channel) slot. A different-AppID replacement deletes the
-- old row and inserts a fresh installation id; the advisory lock closes the
-- gap where two concurrent replacements could otherwise miss each other's new
-- row and update it in place, carrying identity state across robot boundaries.
SELECT pg_advisory_xact_lock(
    hashtextextended(
        (sqlc.arg(workspace_id)::uuid)::text || ':' ||
        (sqlc.arg(agent_id)::uuid)::text || ':sharecrm',
        0
    )
);

-- name: GetShareCRMInstallationOwnerForUpdate :one
-- Reads the current robot identity after LockShareCRMInstallationOwner has
-- serialized the logical owner slot. app_id is non-null for every ShareCRM
-- installation; COALESCE treats malformed legacy config as a different robot,
-- which safely replaces it instead of preserving unknown identity state.
SELECT id, COALESCE(config ->> 'app_id', '')::text AS app_id
FROM channel_installation
WHERE workspace_id = sqlc.arg(workspace_id)
  AND agent_id = sqlc.arg(agent_id)
  AND channel_type = 'sharecrm'
FOR UPDATE;

-- name: DeleteShareCRMInstallationForReplacement :one
-- Retires an installation when the same agent is connected with a DIFFERENT
-- App ID. A ShareCRM user id is only meaningful within one bot/app, so none of
-- the old installation's identity, token, session, or dedup state may cross
-- into the new robot. The caller inserts a fresh installation in the same
-- transaction, giving the replacement a new installation_id.
--
-- Chat sessions themselves remain as history, but their channel bindings are
-- removed. Audit rows remain useful for diagnostics, so their installation
-- references are detached.
WITH retired AS (
    DELETE FROM channel_installation ci
    WHERE ci.id = sqlc.arg(installation_id)
      AND ci.workspace_id = sqlc.arg(workspace_id)
      AND ci.agent_id = sqlc.arg(agent_id)
      AND ci.channel_type = 'sharecrm'
    RETURNING ci.id
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
