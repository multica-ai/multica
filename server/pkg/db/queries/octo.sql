-- Octo IM Bot integration queries. The migration that defines these tables
-- lives at server/migrations/119_octo_integration.up.sql.
--
-- Scoping convention: every public-facing read goes through a workspace-scoped
-- variant where one exists. The lookups that take only a UUID PK (e.g.
-- GetOctoInstallation) are reserved for internal trusted callers (the WS lease
-- scanner, the inbound dispatcher after identity resolution); HTTP handlers
-- should prefer the *InWorkspace forms.

-- =====================
-- octo_installation
-- =====================

-- name: CreateOctoInstallation :one
-- Used when an admin configures a bot for an agent. `bot_token_encrypted` is the
-- ciphertext produced by internal/util/secretbox — never plaintext. The
-- (workspace_id, agent_id) UNIQUE constraint enforces "one Multica Agent ↔ one
-- Octo Bot"; re-configuring goes through UpsertOctoInstallation.
INSERT INTO octo_installation (
    workspace_id, agent_id, bot_token_encrypted,
    robot_id, bot_name, owner_uid, api_url, ws_url, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: UpsertOctoInstallation :one
-- Re-configure path: an admin updates the bot token or the bot is re-registered
-- (cached identity fields refreshed). Forces status back to 'active'. The WS
-- lease is intentionally NOT reset here — the inbound hub owns lease lifecycle.
INSERT INTO octo_installation (
    workspace_id, agent_id, bot_token_encrypted,
    robot_id, bot_name, owner_uid, api_url, ws_url, installer_user_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (workspace_id, agent_id) DO UPDATE SET
    bot_token_encrypted = EXCLUDED.bot_token_encrypted,
    robot_id            = EXCLUDED.robot_id,
    bot_name            = EXCLUDED.bot_name,
    owner_uid           = EXCLUDED.owner_uid,
    api_url             = EXCLUDED.api_url,
    ws_url              = EXCLUDED.ws_url,
    installer_user_id   = EXCLUDED.installer_user_id,
    status              = 'active',
    installed_at        = now(),
    updated_at          = now()
RETURNING *;

-- name: GetOctoInstallation :one
SELECT * FROM octo_installation WHERE id = $1;

-- name: GetOctoInstallationInWorkspace :one
SELECT * FROM octo_installation
WHERE id = $1 AND workspace_id = $2;

-- name: GetOctoInstallationByAgent :one
SELECT * FROM octo_installation
WHERE workspace_id = $1 AND agent_id = $2;

-- name: GetOctoInstallationByRobotID :one
-- Used by the inbound dispatcher to route an event (which carries the bot's
-- robot_id) to its installation row.
SELECT * FROM octo_installation WHERE robot_id = $1;

-- name: ListOctoInstallationsByWorkspace :many
SELECT * FROM octo_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListActiveOctoInstallations :many
-- Boot path for the WebSocket hub: enumerate every active installation so the
-- hub can claim leases and open long connections. Excludes revoked rows.
SELECT * FROM octo_installation
WHERE status = 'active'
ORDER BY created_at ASC;

-- name: SetOctoInstallationStatus :exec
UPDATE octo_installation
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: DeleteOctoInstallation :exec
DELETE FROM octo_installation WHERE id = $1 AND workspace_id = $2;

-- name: AcquireOctoWSLease :one
-- Atomically claims the WebSocket lease for an installation. The CAS predicate
-- accepts the lease when (a) no current holder exists, (b) the holder's lease
-- has expired, or (c) the holder is us (renewal). Returns the row when claimed;
-- returns no rows when another live holder still owns it.
-- Deliberately does NOT touch updated_at: lease acquire/renew is high-frequency
-- operational churn, not a config change. The hub treats an advancing
-- updated_at as a reconfigure signal (and restarts the supervisor), so bumping
-- it here would make every renewal look like a reconfigure and loop forever.
UPDATE octo_installation
SET ws_lease_token      = sqlc.arg('new_token'),
    ws_lease_expires_at = sqlc.arg('new_expires_at')
WHERE id = sqlc.arg('id')
  AND status = 'active'
  AND (
        ws_lease_token IS NULL
        OR ws_lease_expires_at < now()
        OR ws_lease_token = sqlc.arg('new_token')
  )
RETURNING *;

-- name: ReleaseOctoWSLease :exec
-- Drops the lease iff we're still the holder. A racing acquirer that already
-- took over will not have its lease cleared. Like AcquireOctoWSLease, this does
-- NOT bump updated_at (lease churn is not a config change).
UPDATE octo_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- =====================
-- octo_user_binding
-- =====================

-- name: CreateOctoUserBinding :one
-- Records that an Octo uid (per-installation) maps to a Multica user.
--
-- Two structural guarantees:
--   1. The composite FK to member(workspace_id, user_id) makes this statement
--      fail when the redeemer is not (or no longer) a workspace member.
--   2. ON CONFLICT DO UPDATE is gated on `multica_user_id` matching the existing
--      binding, so a second redeemer cannot silently steal an already-bound uid.
--      If the conflict row points at a different user, the UPDATE is skipped and
--      the statement returns ZERO rows — the caller translates that into an
--      "already assigned" error. The same-user case still bumps bound_at so an
--      idempotent re-bind by the original user works.
INSERT INTO octo_user_binding (
    workspace_id, multica_user_id, installation_id, octo_uid
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (installation_id, octo_uid) DO UPDATE SET
    bound_at = now()
WHERE octo_user_binding.multica_user_id = EXCLUDED.multica_user_id
RETURNING *;

-- name: GetOctoUserBindingByUID :one
-- The inbound identity check. A row here means: this uid maps to a Multica user
-- who IS currently a workspace member (the composite FK cascades the binding
-- away when membership is revoked, so a row's existence is itself the
-- membership proof).
SELECT * FROM octo_user_binding
WHERE installation_id = $1 AND octo_uid = $2;

-- name: ListOctoUserBindingsByInstallation :many
SELECT * FROM octo_user_binding
WHERE installation_id = $1
ORDER BY bound_at DESC;

-- name: DeleteOctoUserBinding :exec
DELETE FROM octo_user_binding WHERE id = $1;

-- =====================
-- octo_chat_session_binding
-- =====================

-- name: CreateOctoChatSessionBinding :one
INSERT INTO octo_chat_session_binding (
    chat_session_id, installation_id, octo_channel_id, octo_channel_type
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetOctoChatSessionBinding :one
-- Lookup-by-Octo-channel path. Used by the inbound dispatcher to find the
-- existing chat_session before deciding whether to create one.
SELECT * FROM octo_chat_session_binding
WHERE installation_id = $1 AND octo_channel_id = $2;

-- name: GetOctoChatSessionBindingBySession :one
-- Reverse lookup: given a chat_session_id, find its Octo binding. Used by the
-- outbound patcher to know which (installation, channel) to send/edit when an
-- agent emits output for this session.
SELECT * FROM octo_chat_session_binding
WHERE chat_session_id = $1;

-- =====================
-- octo_inbound_dedup
-- =====================

-- name: ClaimOctoInboundDedup :one
-- The two-phase idempotency gate. The dispatcher uses this BEFORE group filter /
-- identity check / chat-session lookup so a WebSocket reconnect that replays a
-- message cannot re-trigger binding prompts, re-write drop audit rows, or
-- re-touch chat_session.
--
-- Returns the row when a claim is acquired (newly inserted, or re-taken from a
-- stale in-flight claim older than 60s). Returns NO rows when the claim cannot
-- be acquired (terminal processed row, or another worker actively processing
-- within the 60s window). Every successful Claim mints a fresh claim_token for
-- owner fencing; see the table comment and the Lark equivalent for the full
-- rationale.
INSERT INTO octo_inbound_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (installation_id, message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE octo_inbound_dedup.processed_at IS NULL
      AND octo_inbound_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING installation_id, message_id, received_at, processed_at, claim_token;

-- name: MarkOctoInboundDedupProcessed :execrows
-- Locks in a claim as permanently processed, after a durable outcome (drop
-- audit row persisted, OR chat_message + session touched). Invoked INSIDE the
-- chat_message+session transaction for the ingest path so the durable write and
-- the Mark commit atomically. A token mismatch returns zero rows; the caller
-- treats that as a lost claim and rolls back. Guarded by processed_at IS NULL
-- so a successful Mark is idempotent.
UPDATE octo_inbound_dedup
SET processed_at = now()
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: ReleaseOctoInboundDedup :execrows
-- Releases an in-flight claim after an infra error BEFORE any durable side
-- effect, so the retry can re-acquire immediately instead of waiting for the
-- 60s staleness TTL. Guarded by processed_at IS NULL (cannot undo a Mark) and
-- by claim_token (a reclaimed worker cannot delete the new holder's row).
DELETE FROM octo_inbound_dedup
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: PurgeOctoInboundDedup :exec
-- Removes dedup rows older than the supplied cutoff. The vacuum cron calls this
-- with cutoff = now() - INTERVAL '24h'.
DELETE FROM octo_inbound_dedup
WHERE received_at < $1;

-- =====================
-- octo_inbound_audit
-- =====================

-- name: RecordOctoInboundDrop :exec
-- The ONLY write path for events that fail identity check or the group-mention
-- filter. Deliberately accepts no body column — only routing / identity /
-- drop_reason / timestamp.
INSERT INTO octo_inbound_audit (
    installation_id, octo_channel_id, octo_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'),
    sqlc.narg('octo_channel_id'),
    sqlc.narg('octo_message_id'),
    $1
);

-- name: ListOctoInboundAuditByInstallation :many
SELECT * FROM octo_inbound_audit
WHERE installation_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- =====================
-- octo_outbound_message
-- =====================

-- name: CreateOctoOutboundMessage :one
INSERT INTO octo_outbound_message (
    chat_session_id, task_id, octo_channel_id,
    octo_message_id, octo_message_seq, status
) VALUES (
    $1, sqlc.narg('task_id'), $2, $3, $4, $5
)
RETURNING *;

-- name: GetOctoOutboundMessageByTask :one
-- Most edits arrive keyed by task_id (streaming an agent run's output). The
-- partial unique index on (task_id) WHERE task_id IS NOT NULL guarantees this
-- returns at most one row.
SELECT * FROM octo_outbound_message
WHERE task_id = $1;

-- name: UpdateOctoOutboundMessageStatus :exec
UPDATE octo_outbound_message
SET status = $2,
    last_edited_at = now()
WHERE id = $1;

-- =====================
-- octo_binding_token
-- =====================

-- name: CreateOctoBindingToken :one
-- Mints a single-use binding token for an unbound Octo user. The TTL cap
-- (expires_at <= created_at + INTERVAL '15 minutes') is enforced by the DB
-- CHECK, in lockstep with octo.BindingTokenTTL. We store the HASH, not the raw
-- token; the raw value is returned once (in the URL the Bot's reply embeds) and
-- never persisted server-side.
INSERT INTO octo_binding_token (
    token_hash, workspace_id, installation_id, octo_uid, expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: ConsumeOctoBindingToken :one
-- Atomic redemption. Returns the row only if (a) the hash exists, (b) it has not
-- been consumed, and (c) it has not expired. The UPDATE + RETURNING pattern
-- guarantees two simultaneous redemptions cannot both succeed.
UPDATE octo_binding_token
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: PurgeExpiredOctoBindingTokens :exec
DELETE FROM octo_binding_token
WHERE expires_at < $1;
