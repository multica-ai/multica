-- Platform-agnostic inbound channel queries (MUL-3515). These operate on
-- the channel_* tables created in migration 124. Each installation carries
-- a `channel_type` discriminator and a JSONB `config` blob for
-- platform-specific identifiers/credentials; the cross-platform columns
-- stay flat. The Go layer owns building/parsing config — these queries
-- treat it as opaque JSON except for the routing index on config->>'app_id'.
--
-- No foreign keys exist on these tables (MUL-3515 §4): the integrity the
-- old composite FKs enforced (binding workspace matches installation;
-- binding dies with membership / chat_session) is maintained in the
-- application layer via the membership check in the inbound identity step
-- and the *DeleteChannel*BindingsBy* cleanup queries below.

-- =====================
-- channel_installation
-- =====================

-- name: UpsertChannelInstallation :one
-- Install / re-install path. `config` is the opaque per-channel JSONB the
-- Go layer assembles (for feishu: app_id, app_secret_encrypted, tenant_key,
-- bot_open_id, bot_union_id, region). Re-installing the same agent on the
-- same channel_type replaces the whole config and forces status back to
-- 'active'. The conflict key is (workspace_id, agent_id, channel_type) so an
-- agent may hold one installation per channel_type (feishu + slack + ...)
-- without one install clobbering another. The WS lease is intentionally NOT
-- reset here — the inbound hub owns lease lifecycle.
INSERT INTO channel_installation (
    workspace_id, agent_id, channel_type, config, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (workspace_id, agent_id, channel_type) DO UPDATE SET
    channel_type      = EXCLUDED.channel_type,
    config            = EXCLUDED.config,
    installer_user_id = EXCLUDED.installer_user_id,
    status            = 'active',
    installed_at      = now(),
    updated_at        = now()
RETURNING *;

-- name: GetChannelInstallation :one
-- Scoped by channel_type: a per-channel caller (e.g. the Feishu store)
-- must never resolve another channel's installation by guessing its UUID.
SELECT * FROM channel_installation
WHERE id = sqlc.arg('id') AND channel_type = sqlc.arg('channel_type');

-- name: GetChannelInstallationInWorkspace :one
SELECT * FROM channel_installation
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: GetChannelInstallationByAppID :one
-- Inbound routing. The platform event carries only the channel's app
-- identifier (Feishu app_id); the dispatcher's installation resolver routes
-- on (channel_type, config->>'app_id'). Backed by the functional unique
-- index idx_channel_installation_type_appid.
--
-- Both params are named + explicitly typed: `config ->> 'app_id'` makes sqlc
-- attribute a bare `$2` to the JSONB `config` column (it would emit
-- `Config []byte`), so we pin the app_id arg to ::text to get AppID string.
SELECT * FROM channel_installation
WHERE channel_type = sqlc.arg('channel_type')
  AND config ->> 'app_id' = sqlc.arg('app_id')::text;

-- name: ListChannelInstallationsByWorkspace :many
-- Scoped by channel_type so a per-channel management surface (e.g. the Lark
-- installation list) only ever sees its own platform's installations.
SELECT * FROM channel_installation
WHERE workspace_id = sqlc.arg('workspace_id')
  AND channel_type = sqlc.arg('channel_type')
ORDER BY created_at ASC;

-- name: ListActiveChannelInstallations :many
-- Boot path for a per-channel-type inbound hub: every active installation of
-- the given channel_type, so a hub claims leases and opens connections only
-- for its own platform and never supervises another channel's installation.
--
-- The JOINs require the owning workspace and agent rows to still exist.
-- channel_installation has no FK (MUL-3515 §4), so unlike the old
-- lark_installation (which cascaded away on workspace/agent deletion) an
-- installation can be orphaned when its workspace is deleted or its agent is
-- hard-deleted (e.g. runtime teardown). Without this guard the hub would keep
-- opening a WebSocket for a bot whose workspace/agent is gone. The JOIN matches
-- the old ON DELETE CASCADE semantics: it filters on row existence, not agent
-- archival, so an archived-but-present agent's installation is still listed.
SELECT ci.* FROM channel_installation ci
JOIN workspace w ON w.id = ci.workspace_id
JOIN agent a ON a.id = ci.agent_id
WHERE ci.status = 'active'
  AND ci.channel_type = sqlc.arg('channel_type')
ORDER BY ci.created_at ASC;

-- name: ListAllActiveChannelInstallations :many
-- Boot path for the channel-agnostic engine Supervisor (MUL-3620): every
-- active installation across ALL channel types, so one Supervisor drives every
-- platform's connections rather than a per-platform hub. This is the de-
-- hardcoded counterpart of ListActiveChannelInstallations — the Supervisor
-- routes each row to its registered channel.Factory by channel_type, so it
-- never needs to know which platforms exist. Same orphan guard as the per-type
-- query: the workspace + agent JOINs drop installations whose owning rows are
-- gone (channel_installation has no FK, MUL-3515 §4), matching the old ON
-- DELETE CASCADE semantics (row existence, not agent archival).
SELECT ci.* FROM channel_installation ci
JOIN workspace w ON w.id = ci.workspace_id
JOIN agent a ON a.id = ci.agent_id
WHERE ci.status = 'active'
ORDER BY ci.created_at ASC;

-- name: SetChannelInstallationStatus :exec
UPDATE channel_installation
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: SetChannelInstallationConfig :exec
-- Replaces the whole config blob for one installation. Used by the
-- operator backfills (e.g. setting a freshly-fetched bot_union_id) that
-- read-modify-write the JSON in Go and persist it back atomically by id.
UPDATE channel_installation
SET config = $2, updated_at = now()
WHERE id = $1;

-- name: BackfillChannelInstallationRegionToFeishuLark :execrows
-- Operator repair, feishu-only: flip every feishu installation still
-- carrying region='feishu' to 'lark'. Called only on deployments whose
-- legacy global base-URL override pointed at Lark international. Idempotent.
UPDATE channel_installation
SET config     = jsonb_set(config, '{region}', '"lark"'),
    updated_at = now()
WHERE channel_type = 'feishu'
  AND config ->> 'region' = 'feishu';

-- name: AcquireChannelWSLease :one
-- Atomically claims the WebSocket lease. CAS predicate accepts when no
-- holder exists, the holder expired, or the holder is us (renewal).
UPDATE channel_installation
SET ws_lease_token       = sqlc.arg('new_token'),
    ws_lease_expires_at  = sqlc.arg('new_expires_at'),
    updated_at           = now()
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseChannelWSLease :exec
-- Drops the lease iff we are still the holder.
UPDATE channel_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- =====================
-- channel_user_binding
-- =====================

-- name: CreateChannelUserBinding :one
-- Records that a platform user id (per-installation; Feishu open_id) maps
-- to a Multica user. The old composite member-FK is gone, so this no
-- longer fails when the redeemer is not a workspace member — the caller
-- (BindingTokenService.RedeemAndBind) validates membership explicitly
-- before calling. ON CONFLICT DO UPDATE is still gated on multica_user_id
-- matching, so a second redeemer cannot steal an already-bound user id;
-- a cross-user conflict updates zero rows and the caller maps that to
-- ErrBindingAlreadyAssigned. config carries secondary identity (union_id).
INSERT INTO channel_user_binding (
    workspace_id, multica_user_id, installation_id,
    channel_type, channel_user_id, config
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (installation_id, channel_user_id) DO UPDATE SET
    -- jsonb_strip_nulls(EXCLUDED.config) preserves the old lark semantics
    -- `union_id = COALESCE(EXCLUDED.union_id, lark_user_binding.union_id)`:
    -- a re-bind that carries `{"union_id": null}` (or omits the key) must NOT
    -- erase a union_id we already captured. Only non-null incoming keys win.
    config   = channel_user_binding.config || jsonb_strip_nulls(EXCLUDED.config),
    bound_at = now()
WHERE channel_user_binding.multica_user_id = EXCLUDED.multica_user_id
RETURNING *;

-- name: GetChannelUserBindingByUserID :one
-- The inbound identity lookup: does this platform user id map to a Multica
-- user for this installation? With the member-FK removed, a row's
-- existence no longer proves current workspace membership — the dispatcher
-- re-checks membership after this lookup.
SELECT * FROM channel_user_binding
WHERE installation_id = $1 AND channel_user_id = $2;

-- name: DeleteChannelUserBindingsByWorkspaceMember :exec
-- Application-layer integrity (replaces the old member-FK ON DELETE
-- CASCADE): prune every binding for a user who has been removed from a
-- workspace, across all installations in that workspace.
DELETE FROM channel_user_binding
WHERE workspace_id = $1 AND multica_user_id = $2;

-- =====================
-- channel_chat_session_binding
-- =====================

-- name: CreateChannelChatSessionBinding :one
-- channel_chat_id is the session-isolation key (one chat_session per
-- (installation_id, channel_chat_id)): Feishu passes the chat id; Slack passes
-- a stable key that, for channels, includes the thread root so each @bot thread
-- is its own session. config carries any platform-specific outbound routing the
-- key alone does not (e.g. Slack's real channel_id when the key is composite);
-- it is opaque to the shared session service.
INSERT INTO channel_chat_session_binding (
    chat_session_id, installation_id, channel_type, channel_chat_id, chat_type, config
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetChannelChatSessionBinding :one
-- Lookup-by-channel-chat: the inbound dispatcher finds the existing
-- chat_session before deciding whether to create one.
SELECT * FROM channel_chat_session_binding
WHERE installation_id = $1 AND channel_chat_id = $2;

-- name: GetChannelChatSessionBindingBySession :one
-- Reverse lookup for the outbound patcher: given a chat_session_id, find
-- its channel binding to know which (installation, chat_id) to send to.
-- Scoped by channel_type so a future non-Feishu binding on the same
-- chat_session is never treated as a Feishu reply target.
SELECT * FROM channel_chat_session_binding
WHERE chat_session_id = sqlc.arg('chat_session_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: UpdateChannelChatSessionBindingReplyTarget :exec
-- Records the most recent inbound trigger message + thread so the decoupled
-- outbound patcher can thread its reply back into the originating topic.
UPDATE channel_chat_session_binding
SET last_message_id = sqlc.narg('last_message_id'),
    last_thread_id  = sqlc.narg('last_thread_id')
WHERE chat_session_id = $1;

-- name: DeleteChannelChatSessionBindingBySession :exec
-- Application-layer integrity (replaces the old chat_session-FK ON DELETE
-- CASCADE): drop the binding when its chat_session is deleted.
DELETE FROM channel_chat_session_binding
WHERE chat_session_id = $1;

-- =====================
-- channel_inbound_message_dedup
-- =====================

-- name: ClaimChannelInboundDedup :one
-- Two-phase idempotency gate with owner fencing. Returns the row when a
-- claim is acquired (fresh insert, or stale-reclaim of an in-flight claim
-- older than 60s); returns no rows when terminal (processed) or actively
-- in-flight. Every claim mints a fresh claim_token; Mark/Release are
-- fenced on it. See the table comment in migration 124 / the lark
-- predecessor for the full invariant set.
INSERT INTO channel_inbound_message_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (installation_id, message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE channel_inbound_message_dedup.processed_at IS NULL
      AND channel_inbound_message_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING installation_id, message_id, received_at, processed_at, claim_token;

-- name: MarkChannelInboundDedupProcessed :execrows
-- Locks a claim in as permanently processed after a durable outcome.
-- Invoked inside the chat_message tx (via qtx) on the ingest path so the
-- durable write and the Mark commit atomically. Token mismatch returns
-- zero rows (a reclaim happened); the caller rolls back its in-tx write.
UPDATE channel_inbound_message_dedup
SET processed_at = now()
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: ReleaseChannelInboundDedup :execrows
-- Releases an in-flight claim when an infra error occurred before any
-- durable side effect, so a retry can re-acquire immediately. Fenced on
-- processed_at IS NULL and claim_token.
DELETE FROM channel_inbound_message_dedup
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: PurgeChannelInboundDedup :exec
-- Vacuum job: remove dedup rows older than the supplied cutoff (e.g. 24h).
DELETE FROM channel_inbound_message_dedup
WHERE received_at < $1;

-- =====================
-- channel_inbound_audit
-- =====================

-- name: RecordChannelInboundDrop :exec
-- The only write path for dropped events. Deliberately carries no body
-- column — only routing / identity / drop_reason / timestamp.
INSERT INTO channel_inbound_audit (
    installation_id, channel_type, channel_chat_id, event_type,
    channel_event_id, channel_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'),
    $1,
    sqlc.narg('channel_chat_id'),
    $2,
    sqlc.narg('channel_event_id'),
    sqlc.narg('channel_message_id'),
    $3
);

-- name: ListChannelInboundAuditByInstallation :many
SELECT * FROM channel_inbound_audit
WHERE installation_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- =====================
-- channel_outbound_card_message
-- =====================

-- name: CreateChannelOutboundCardMessage :one
INSERT INTO channel_outbound_card_message (
    chat_session_id, task_id, channel_type, channel_chat_id,
    channel_card_message_id, status
) VALUES (
    $1, sqlc.narg('task_id'), $2, $3, $4, $5
)
RETURNING *;

-- name: GetChannelOutboundCardByTask :one
-- The partial unique index on (task_id) WHERE task_id IS NOT NULL
-- guarantees at most one row. Scoped by channel_type so a future non-Feishu
-- card for the same task is not patched as a Feishu card.
SELECT * FROM channel_outbound_card_message
WHERE task_id = sqlc.arg('task_id')
  AND channel_type = sqlc.arg('channel_type');

-- name: UpdateChannelOutboundCardStatus :exec
UPDATE channel_outbound_card_message
SET status = $2,
    last_patched_at = now()
WHERE id = $1;

-- =====================
-- channel_binding_token
-- =====================

-- name: CreateChannelBindingToken :one
-- Mints a single-use binding token for an unbound platform user. TTL cap
-- (15 min) enforced by the table CHECK in lockstep with
-- channel.BindingTokenTTL. The HASH is stored, never the raw token.
INSERT INTO channel_binding_token (
    token_hash, workspace_id, installation_id, channel_type,
    channel_user_id, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: ConsumeChannelBindingToken :one
-- Atomic redemption: returns the row only if the hash exists, is
-- unconsumed, and unexpired. Two simultaneous redemptions cannot both win.
UPDATE channel_binding_token
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: PurgeExpiredChannelBindingTokens :exec
DELETE FROM channel_binding_token
WHERE expires_at < $1;
