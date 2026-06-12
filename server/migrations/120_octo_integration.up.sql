-- Octo IM Bot integration: per-agent bot installations, user/chat bindings,
-- inbound dedup + drop audit, outbound message mapping, and short-lived member
-- binding tokens.
--
-- This mirrors the Lark integration (migration 109) structurally, with these
-- Octo-specific differences:
--   * Octo authenticates with a single bot token (bf_*), not an app_id +
--     app_secret pair. The token is stored encrypted (application-layer
--     secretbox); the DB never sees plaintext.
--   * No region column (single private deployment, no feishu/lark split).
--   * Outbound is plain text / markdown with WuKongIM message-edit for
--     streaming, so the outbound table stores (message_id, message_seq) of the
--     sent message rather than an interactive-card id.
--   * `chat_session` is reused as-is; Octo routes through a separate
--     `octo_chat_session_binding` rather than adding columns to chat_session.

-- =====================
-- octo_installation
-- =====================
-- One row per (workspace, agent) — each Multica Agent owns at most one Octo
-- Bot. `bot_token_encrypted` is the ciphertext produced by the application-
-- layer secretbox helper; never plaintext. `robot_id`, `bot_name`, `owner_uid`,
-- `api_url`, and `ws_url` are cached from the register response so the hub can
-- open connections without an extra round-trip.
CREATE TABLE octo_installation (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id              UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    -- Ciphertext of the Octo bot token (bf_*). Application-layer secretbox.
    -- DB never sees plaintext; a dump leaks ciphertext only.
    bot_token_encrypted   BYTEA NOT NULL,
    -- Bot identity, cached from the /v1/bot/register response.
    robot_id              TEXT NOT NULL,
    bot_name              TEXT NOT NULL DEFAULT '',
    owner_uid             TEXT NOT NULL DEFAULT '',
    api_url               TEXT NOT NULL,
    ws_url                TEXT NOT NULL DEFAULT '',
    installer_user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    status                TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    -- WS ownership lease: only the server instance holding a non-expired lease
    -- may keep the WebSocket open for this installation. Prevents duplicate
    -- consumption when multiple replicas are deployed.
    ws_lease_token        TEXT,
    ws_lease_expires_at   TIMESTAMPTZ,
    installed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id),
    -- robot_id is globally unique per Octo deployment; used by the inbound
    -- dispatcher to route an event (which carries the bot's robot_id) to its
    -- installation row.
    UNIQUE (robot_id),
    -- Composite key target for the composite FK on octo_user_binding
    -- (installation_id, workspace_id) — guarantees a binding's workspace always
    -- matches the workspace of its installation.
    UNIQUE (id, workspace_id)
);

CREATE INDEX idx_octo_installation_workspace ON octo_installation(workspace_id);
CREATE INDEX idx_octo_installation_agent ON octo_installation(agent_id);
-- Used by the lease scanner to find leases due for renewal / takeover.
CREATE INDEX idx_octo_installation_lease ON octo_installation(ws_lease_expires_at)
    WHERE status = 'active';

-- =====================
-- octo_user_binding
-- =====================
-- Maps an Octo `uid` to a Multica user, per-installation. The binding is keyed
-- on (installation, uid). Two composite FKs protect the "unbound or
-- non-workspace members never leak content into chat_session" rule from
-- drifting if the application layer regresses:
--
--   1. The composite FK on (installation_id, workspace_id) targets
--      octo_installation(id, workspace_id), so a binding row cannot claim a
--      workspace different from its installation's workspace.
--
--   2. The composite FK on (workspace_id, multica_user_id) targets
--      member(workspace_id, user_id) with ON DELETE CASCADE, so when a Multica
--      user is removed from the workspace the stale Octo binding is removed in
--      the same transaction.
CREATE TABLE octo_user_binding (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    multica_user_id  UUID NOT NULL,
    installation_id  UUID NOT NULL,
    octo_uid         TEXT NOT NULL,
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, octo_uid),
    CONSTRAINT octo_user_binding_installation_fk
        FOREIGN KEY (installation_id, workspace_id)
        REFERENCES octo_installation(id, workspace_id)
        ON DELETE CASCADE,
    CONSTRAINT octo_user_binding_member_fk
        FOREIGN KEY (workspace_id, multica_user_id)
        REFERENCES member(workspace_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_octo_user_binding_user
    ON octo_user_binding(multica_user_id, workspace_id);
-- No explicit index on (installation_id, octo_uid): the UNIQUE constraint above
-- already creates one, and that is exactly the key GetOctoUserBindingByUID reads.
-- (Unlike Lark, whose second index is on the DIFFERENT key (workspace_id,
-- lark_open_id); Octo has no workspace-scoped uid lookup, so nothing to add.)

-- =====================
-- octo_chat_session_binding
-- =====================
-- One Octo channel (`channel_id`) ↔ one Multica `chat_session`. We keep
-- `octo_channel_type` for product behavior (group sessions only ingest @-Bot /
-- reply-Bot messages; DM ingests everything).
CREATE TABLE octo_chat_session_binding (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id    UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    installation_id    UUID NOT NULL REFERENCES octo_installation(id) ON DELETE CASCADE,
    octo_channel_id    TEXT NOT NULL,
    -- WuKongIM channel_type: 1=DM, 2=group, 5=community topic.
    octo_channel_type  SMALLINT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, octo_channel_id),
    UNIQUE (chat_session_id)
);

-- No explicit index on (chat_session_id): the UNIQUE constraint above already
-- creates one, which fully serves the reverse lookup
-- GetOctoChatSessionBindingBySession.

-- =====================
-- octo_inbound_dedup
-- =====================
-- Idempotency for Octo inbound events. WebSocket reconnects can replay
-- recently-delivered messages; we keep a window of message_ids to short-circuit
-- replays before any business logic runs. A periodic vacuum job trims old rows.
--
-- Two-phase semantics with owner fencing (see ClaimOctoInboundDedup /
-- MarkOctoInboundDedupProcessed / ReleaseOctoInboundDedup in queries/octo.sql):
--
--   processed_at IS NULL     → in-flight claim. The dispatcher holds a row but
--     has not yet reached a durable outcome. Re-claimable after the staleness
--     TTL so a crash does not permanently swallow a replay.
--   processed_at IS NOT NULL → terminal. Future replays are dropped.
--   claim_token              → owner fence. Each Claim mints a fresh UUID; Mark
--     and Release only succeed when the supplied token matches, closing the
--     stale-reclaim race and the mark window (see the Lark migration comment for
--     the full rationale — the semantics are identical).
--
-- Keyed on (installation_id, message_id) so the same message_id from different
-- installations never collides.
CREATE TABLE octo_inbound_dedup (
    installation_id  UUID NOT NULL,
    message_id       TEXT NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    claim_token      UUID NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (installation_id, message_id)
);

CREATE INDEX idx_octo_inbound_dedup_received
    ON octo_inbound_dedup(received_at);

-- =====================
-- octo_inbound_audit
-- =====================
-- Non-content audit log for events that DID arrive but were intentionally
-- dropped (group message without @, unbound user, non-workspace member,
-- duplicate, etc.). NEVER stores message body — only routing + identity +
-- drop_reason + timestamp.
CREATE TABLE octo_inbound_audit (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID REFERENCES octo_installation(id) ON DELETE SET NULL,
    octo_channel_id   TEXT,
    octo_message_id   TEXT,
    -- Open-ended TEXT (not an enum) so new drop reasons can be added in
    -- application code without a schema migration. Convention: snake_case.
    -- Known values: unbound_user, non_workspace_member, not_addressed_in_group,
    -- duplicate, revoked_installation, invalid_event.
    drop_reason       TEXT NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_octo_inbound_audit_installation
    ON octo_inbound_audit(installation_id, received_at DESC);
CREATE INDEX idx_octo_inbound_audit_reason
    ON octo_inbound_audit(drop_reason, received_at DESC);

-- =====================
-- octo_outbound_message
-- =====================
-- Maps a Multica task to the Octo message we sent for it, so streaming output
-- can edit (message/edit) the same message rather than spamming new ones.
-- Per-task, not per-session — a chat_session can host many runs and a
-- session-level field would alias messages across runs. `task_id` may be NULL
-- for a bootstrap message before a task exists; the partial UNIQUE index keeps
-- task↔message 1:1 once a task exists.
CREATE TABLE octo_outbound_message (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id    UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    task_id            UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    octo_channel_id    TEXT NOT NULL,
    octo_message_id    TEXT NOT NULL,
    octo_message_seq   BIGINT NOT NULL DEFAULT 0,
    status             TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'streaming', 'final', 'error')),
    last_edited_at     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_octo_outbound_message_task
    ON octo_outbound_message(task_id)
    WHERE task_id IS NOT NULL;
CREATE INDEX idx_octo_outbound_message_session
    ON octo_outbound_message(chat_session_id, created_at DESC);

-- =====================
-- octo_binding_token
-- =====================
-- Short-lived (≤ 15 min), single-use token for the "you're not bound yet, click
-- here" flow that links an Octo `uid` to a Multica user. The hash (not the raw
-- token) is stored so a DB leak doesn't grant binding capability. Replay is
-- blocked by `consumed_at IS NOT NULL`.
CREATE TABLE octo_binding_token (
    token_hash       TEXT PRIMARY KEY,
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id  UUID NOT NULL REFERENCES octo_installation(id) ON DELETE CASCADE,
    octo_uid         TEXT NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    consumed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Belt-and-braces with the application-layer cap (octo.BindingTokenTTL =
    -- 15 minutes). Refuses any row whose lifetime exceeds the product cap.
    CONSTRAINT octo_binding_token_ttl_cap
        CHECK (expires_at <= created_at + INTERVAL '15 minutes')
);

CREATE INDEX idx_octo_binding_token_installation
    ON octo_binding_token(installation_id, expires_at);

-- =====================
-- issue.origin_type
-- =====================
-- Allow the Octo `/issue` command path to stamp issues with
-- origin_type='octo_chat' + origin_id=<chat_session.id>. Without this entry the
-- CHECK rejects the insert (SQLSTATE 23514) and the connector takes an infra
-- error per the Lark MUL-2671 precedent.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'octo_chat'));
