-- Lark (飞书) Bot integration queries. The migration that defines these
-- tables lives at server/migrations/109_lark_integration.up.sql; the
-- architectural boundaries the package enforces on top of them are
-- documented in server/internal/integrations/lark/doc.go.
--
-- Scoping convention: every public-facing read goes through a
-- workspace-scoped variant where one exists. The lookups that take only
-- a UUID PK (e.g. GetLarkInstallation) are reserved for internal trusted
-- callers (the WS lease scanner, the inbound dispatcher after identity
-- resolution); HTTP handlers should prefer the *InWorkspace forms.

-- =====================
-- lark_installation
-- =====================

-- name: CreateLarkInstallation :one
-- Used by the OAuth callback. `app_secret_encrypted` is the ciphertext
-- produced by internal/util/secretbox — never plaintext. The
-- (workspace_id, agent_id) UNIQUE constraint enforces the spec rule
-- "one Multica Agent ↔ one Lark Bot"; re-installing on the same agent
-- goes through UpsertLarkInstallation instead.
INSERT INTO lark_installation (
    workspace_id, agent_id, app_id, app_secret_encrypted,
    tenant_key, bot_open_id, bot_union_id, installer_user_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('tenant_key'), $5, sqlc.narg('bot_union_id'), $6
)
RETURNING *;

-- name: UpsertLarkInstallation :one
-- Re-install path: a user who already bound this agent to Lark scans
-- the QR again (e.g. they rotated their Lark app secret, or revoked +
-- reinstalled). We refresh the app credentials, bot identity, and
-- installer attribution, and force status back to 'active'. The WS
-- lease is intentionally NOT reset here — the inbound hub owns lease
-- lifecycle.
INSERT INTO lark_installation (
    workspace_id, agent_id, app_id, app_secret_encrypted,
    tenant_key, bot_open_id, bot_union_id, installer_user_id, region
) VALUES (
    $1, $2, $3, $4, sqlc.narg('tenant_key'), $5, sqlc.narg('bot_union_id'), $6, sqlc.arg('region')
)
ON CONFLICT (workspace_id, agent_id) DO UPDATE SET
    app_id               = EXCLUDED.app_id,
    app_secret_encrypted = EXCLUDED.app_secret_encrypted,
    tenant_key           = EXCLUDED.tenant_key,
    bot_open_id          = EXCLUDED.bot_open_id,
    bot_union_id         = EXCLUDED.bot_union_id,
    installer_user_id    = EXCLUDED.installer_user_id,
    region               = EXCLUDED.region,
    status               = 'active',
    installed_at         = now(),
    updated_at           = now()
RETURNING *;

-- name: BackfillLarkInstallationRegionToLark :execrows
-- Upgrade repair: flip every installation still carrying the migration-116
-- default ('feishu') to 'lark'. Called ONLY by
-- BackfillRegionFromLegacyOverride, and ONLY when the deployment's global
-- base-URL override pointed at Lark international — on such a deployment the
-- whole integration talked to open.larksuite.com, so every existing install
-- is really Lark and the migration's mainland default mislabels it.
-- Idempotent: once flipped there is nothing left at 'feishu' to update, and
-- new installs already carry the device-flow-detected region.
UPDATE lark_installation
SET region     = 'lark',
    updated_at = now()
WHERE region = 'feishu';

-- name: SetLarkInstallationBotUnionID :exec
-- Operator-only backfill for installations created before the
-- bot_union_id column existed (migration 112). Production reads do
-- NOT use this — finishSuccess writes union_id during install, and
-- the upsert path writes it on re-install. Kept as a focused single-
-- column UPDATE so the backfill cannot accidentally overwrite app
-- credentials, status, or lease state.
UPDATE lark_installation
SET bot_union_id = $2,
    updated_at   = now()
WHERE id = $1;

-- name: GetLarkInstallation :one
SELECT * FROM lark_installation WHERE id = $1;

-- name: GetLarkInstallationInWorkspace :one
SELECT * FROM lark_installation
WHERE id = $1 AND workspace_id = $2;

-- name: GetLarkInstallationByAgent :one
SELECT * FROM lark_installation
WHERE workspace_id = $1 AND agent_id = $2;

-- name: GetLarkInstallationByAppID :one
-- Used by the OAuth callback to detect re-install vs first-install,
-- and by the inbound dispatcher to route an event payload (which only
-- carries app_id) to its installation row.
SELECT * FROM lark_installation WHERE app_id = $1;

-- name: ListLarkInstallationsByWorkspace :many
SELECT * FROM lark_installation
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: ListActiveLarkInstallations :many
-- Boot path for the WebSocket hub: enumerate every active installation
-- so the hub can claim leases and open long connections. Excludes
-- revoked rows — their WS should already be torn down.
SELECT * FROM lark_installation
WHERE status = 'active'
ORDER BY created_at ASC;

-- name: SetLarkInstallationStatus :exec
UPDATE lark_installation
SET status = $2, updated_at = now()
WHERE id = $1;

-- name: AcquireLarkWSLease :one
-- Atomically claims the WebSocket lease for an installation. The CAS
-- predicate accepts the lease when (a) no current holder exists, (b)
-- the holder's lease has expired, or (c) the holder is us (renewal).
-- Returns the row when the lease was successfully claimed; returns no
-- rows when another live holder still owns it.
UPDATE lark_installation
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

-- name: ReleaseLarkWSLease :exec
-- Drops the lease iff we're still the holder. A racing acquirer that
-- already took over will not have its lease cleared.
UPDATE lark_installation
SET ws_lease_token      = NULL,
    ws_lease_expires_at = NULL,
    updated_at          = now()
WHERE id = $1
  AND ws_lease_token = sqlc.arg('current_token');

-- =====================
-- lark_user_binding
-- =====================

-- name: CreateLarkUserBinding :one
-- Records that a Lark open_id (per-installation) maps to a Multica
-- user.
--
-- Two structural guarantees:
--   1. The composite FK to member(workspace_id, user_id) makes this
--      statement fail when the redeemer is not (or no longer) a
--      workspace member — that is §4.3 of the design.
--   2. ON CONFLICT DO UPDATE is gated on `multica_user_id` matching
--      the existing binding, so a second redeemer holding their own
--      valid binding token CANNOT silently steal an already-bound
--      open_id. If the conflict row points at a different user, the
--      UPDATE is skipped and the statement returns ZERO rows — the
--      caller (lark.BindingTokenService.RedeemAndBind) translates
--      that into ErrBindingAlreadyAssigned.
--
-- The same-user case still updates metadata (union_id refresh,
-- bound_at bump) so an idempotent re-bind by the original user
-- continues to work; only a cross-user re-assignment is rejected.
-- True account changes must go through an explicit unbind flow, not
-- through a binding token.
INSERT INTO lark_user_binding (
    workspace_id, multica_user_id, installation_id, lark_open_id, union_id
) VALUES (
    $1, $2, $3, $4, sqlc.narg('union_id')
)
ON CONFLICT (installation_id, lark_open_id) DO UPDATE SET
    union_id = COALESCE(EXCLUDED.union_id, lark_user_binding.union_id),
    bound_at = now()
WHERE lark_user_binding.multica_user_id = EXCLUDED.multica_user_id
RETURNING *;

-- name: GetLarkUserBindingByOpenID :one
-- The inbound identity check. A row here means: this open_id maps to a
-- Multica user who IS currently a workspace member (the composite FK
-- cascades the binding away when membership is revoked, so a row's
-- existence is itself the membership proof).
SELECT * FROM lark_user_binding
WHERE installation_id = $1 AND lark_open_id = $2;

-- name: ListLarkUserBindingsByInstallation :many
SELECT * FROM lark_user_binding
WHERE installation_id = $1
ORDER BY bound_at DESC;

-- name: DeleteLarkUserBinding :exec
DELETE FROM lark_user_binding WHERE id = $1;

-- =====================
-- lark_chat_session_binding
-- =====================

-- name: CreateLarkChatSessionBinding :one
INSERT INTO lark_chat_session_binding (
    chat_session_id, installation_id, lark_chat_id, lark_chat_type
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetLarkChatSessionBinding :one
-- Lookup-by-Lark-chat path. Used by the inbound dispatcher to find the
-- existing chat_session before deciding whether to create one. The
-- UNIQUE (installation_id, lark_chat_id) constraint means at most one
-- row matches.
SELECT * FROM lark_chat_session_binding
WHERE installation_id = $1 AND lark_chat_id = $2;

-- name: GetLarkChatSessionBindingBySession :one
-- Reverse lookup: given a chat_session_id, find its Lark binding. Used
-- by the outbound card patcher to know which (installation, chat_id)
-- to PATCH when an agent emits a stream event for this session.
SELECT * FROM lark_chat_session_binding
WHERE chat_session_id = $1;

-- =====================
-- lark_inbound_message_dedup
-- =====================

-- name: ClaimLarkInboundDedup :one
-- The two-phase idempotency gate. The dispatcher uses this BEFORE
-- group filter / identity check / chat-session lookup so a WebSocket
-- reconnect that replays an event cannot re-trigger binding prompts,
-- re-write drop audit rows, or re-touch chat_session.
--
-- Returns the row when a claim is acquired:
--   - newly inserted (first delivery of this message_id), OR
--   - re-taken from a stale in-flight claim. A claim is stale when
--     processed_at IS NULL AND received_at is older than 60 seconds —
--     the previous worker crashed or lost its DB connection between
--     claim and finalize, and a retry should be allowed to proceed.
--
-- Returns NO rows (pgx.ErrNoRows) when the claim cannot be acquired:
--   - the row exists with processed_at IS NOT NULL (terminal: prior
--     attempt reached a durable outcome), OR
--   - the row exists with processed_at IS NULL AND received_at within
--     the last 60 seconds (another worker is actively processing).
--
-- Owner fencing: every successful Claim mints a fresh UUID into
-- `claim_token`. The Caller passes that token to MarkLarkInbound-
-- DedupProcessed / ReleaseLarkInboundDedup; mismatched tokens are
-- ignored. A stale-reclaim that re-takes the row ROTATES the token,
-- so the previous (slow but still alive) worker can no longer Mark
-- the row — its same-tx Mark returns zero rows and the chat_message
-- write rolls back. See lark_inbound_message_dedup table comment.
--
-- The dispatcher MUST follow up every successful claim with exactly one
-- of MarkLarkInboundDedupProcessed (durable outcome) or
-- ReleaseLarkInboundDedup (infra failure before durable outcome),
-- supplying the returned claim_token. Otherwise the row sits as an
-- in-flight claim and the next replay attempt must wait for the
-- staleness TTL.
INSERT INTO lark_inbound_message_dedup (installation_id, message_id, claim_token)
VALUES ($1, $2, gen_random_uuid())
ON CONFLICT (installation_id, message_id) DO UPDATE
    SET received_at = now(),
        claim_token = gen_random_uuid()
    WHERE lark_inbound_message_dedup.processed_at IS NULL
      AND lark_inbound_message_dedup.received_at < now() - INTERVAL '60 seconds'
RETURNING installation_id, message_id, received_at, processed_at, claim_token;

-- name: MarkLarkInboundDedupProcessed :execrows
-- Locks in a claim as permanently processed. Called by the dispatcher
-- after a durable outcome has been reached:
--   - a drop audit row was persisted (group filter / unbound user /
--     revoked / invalid event), OR
--   - chat_message + chat_session.updated_at were committed (ingest
--     path, including ingest paths that subsequently fail at issue
--     creation / task enqueue — the user-visible message is already in
--     the session).
-- For the chat_message ingest path the dispatcher invokes this query
-- INSIDE the chat_message+session transaction (via qtx), so the
-- durable write and the Mark commit atomically. A token mismatch
-- (another worker has re-claimed the row in the meantime) returns
-- zero rows; the caller treats that as a lost claim and rolls back the
-- in-tx invocation, so no second chat_message is written.
--
-- Guarded by processed_at IS NULL so a successful Mark is itself
-- idempotent: replaying it cannot resurrect a row that was already
-- terminal.
UPDATE lark_inbound_message_dedup
SET processed_at = now()
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: ReleaseLarkInboundDedup :execrows
-- Releases an in-flight claim. Called by the dispatcher when an infra
-- error occurred BEFORE any durable side effect (e.g. EnsureChatSession
-- or AppendUserMessage returned an error and its transaction rolled
-- back). Deleting the row lets the WS adapter's retry re-acquire the
-- claim immediately, instead of waiting for the 60-second staleness
-- TTL. Guarded by processed_at IS NULL so an out-of-order Release
-- cannot undo a Mark; guarded by claim_token so a slow-but-alive worker
-- whose claim was reclaimed cannot delete the new holder's row.
DELETE FROM lark_inbound_message_dedup
WHERE installation_id = $1
  AND message_id = $2
  AND claim_token = $3
  AND processed_at IS NULL;

-- name: PurgeLarkInboundDedup :exec
-- Removes dedup rows older than the supplied cutoff. The vacuum job
-- (separate cron) calls this with cutoff = now() - INTERVAL '24h'.
-- Sweeps both processed and (very old) abandoned in-flight rows.
DELETE FROM lark_inbound_message_dedup
WHERE received_at < $1;

-- =====================
-- lark_inbound_audit
-- =====================

-- name: RecordLarkInboundDrop :exec
-- The ONLY write path for events that fail identity check or the
-- group-mention filter. Deliberately accepts no body column — the
-- AuditLogger interface in internal/integrations/lark mirrors that
-- shape so a caller cannot accidentally hand a body to this row.
INSERT INTO lark_inbound_audit (
    installation_id, lark_chat_id, event_type,
    lark_event_id, lark_message_id, drop_reason
) VALUES (
    sqlc.narg('installation_id'),
    sqlc.narg('lark_chat_id'),
    $1,
    sqlc.narg('lark_event_id'),
    sqlc.narg('lark_message_id'),
    $2
);

-- name: ListLarkInboundAuditByInstallation :many
-- Ops debugging view; paged via the (installation_id, received_at) idx.
SELECT * FROM lark_inbound_audit
WHERE installation_id = $1
ORDER BY received_at DESC
LIMIT $2 OFFSET $3;

-- =====================
-- lark_outbound_card_message
-- =====================

-- name: CreateLarkOutboundCardMessage :one
INSERT INTO lark_outbound_card_message (
    chat_session_id, task_id, lark_chat_id, lark_card_message_id, status
) VALUES (
    $1, sqlc.narg('task_id'), $2, $3, $4
)
RETURNING *;

-- name: GetLarkOutboundCardByTask :one
-- Most card patches arrive keyed by task_id (we're streaming an agent
-- run's output). The partial unique index on (task_id) WHERE task_id IS
-- NOT NULL guarantees this returns at most one row.
SELECT * FROM lark_outbound_card_message
WHERE task_id = $1;

-- name: UpdateLarkOutboundCardStatus :exec
UPDATE lark_outbound_card_message
SET status = $2,
    last_patched_at = now()
WHERE id = $1;

-- =====================
-- lark_binding_token
-- =====================

-- name: CreateLarkBindingToken :one
-- Mints a single-use binding token for an unbound Lark user. The TTL
-- cap (`expires_at <= created_at + INTERVAL '15 minutes'`) is enforced
-- by the DB CHECK on the table, in lockstep with lark.BindingTokenTTL.
-- We store the HASH, not the raw token; the raw value is returned to
-- the caller exactly once (in the URL it embeds in the Bot's reply
-- card) and never persisted server-side.
INSERT INTO lark_binding_token (
    token_hash, workspace_id, installation_id, lark_open_id, expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: ConsumeLarkBindingToken :one
-- Atomic redemption. Returns the row only if (a) the hash exists, (b)
-- it has not been consumed, and (c) it has not expired. The UPDATE +
-- RETURNING pattern guarantees that two simultaneous redemptions of
-- the same token cannot both succeed — exactly one row update wins,
-- the other sees zero rows.
UPDATE lark_binding_token
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
RETURNING *;

-- name: PurgeExpiredLarkBindingTokens :exec
-- Tokens are tiny but unbounded over time. The same vacuum cron that
-- handles dedup can sweep these too.
DELETE FROM lark_binding_token
WHERE expires_at < $1;
