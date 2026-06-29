-- Generalize the Feishu/Lark-specific integration tables into
-- platform-agnostic channel_* tables (MUL-3515, parent MUL-3506). Each
-- lark_* table gains a `channel_type` discriminator and moves its
-- platform-specific identifiers/config into a JSONB `config` column; the
-- cross-platform columns stay flat. Existing Feishu rows are backfilled
-- with channel_type='feishu'.
--
-- Two hard rules from the design:
--
--   * NO foreign keys and NO cascades (MUL-3515 §4). The lark_* tables
--     leaned on composite FKs to enforce "a binding's workspace matches
--     its installation" and "a binding dies when workspace membership is
--     revoked / a chat_session is deleted". Those integrity rules now
--     live in the application layer (the cutover PR adds the membership
--     check + cleanup). The columns are kept so the app can still join,
--     but the database enforces nothing.
--
--   * The lark_* tables are NOT dropped here — that happens in a later
--     migration once the Go cutover has landed, so this migration can
--     ship green on its own. This migration only ADDS channel_* and
--     copies the data forward.
--
--   * ROLLOUT. This backfill is a one-time copy and the Lark hub is an
--     in-process goroutine, so a cutover has TWO independent invariants:
--       (a) channel_* must exist BEFORE any new (channel_*) build serves the
--           HTTP paths that touch it — installation list/install/revoke,
--           chat-session delete, member revoke — or those paths 500.
--       (b) the OLD (lark_*) hub and the NEW (channel_*) hub must never run at
--           once: each would claim the same Feishu bot's WS lease on its own
--           table and open a duplicate connection, double-processing inbound
--           events (duplicate messages / /issue / runs). The two table sets
--           never cross-deduplicate.
--
--     SELF-HOST (Docker Compose / Helm) satisfies both automatically and needs
--     NO flags or manual steps. The backend entrypoint runs `migrate up`
--     before the server starts, so channel_* exists before the new build
--     serves (a); the deployment is single-replica `Recreate`, so the old pod
--     (and its hub) fully stops before the new pod starts (b). A normal
--     version upgrade is a clean cutover. Only a self-host re-tuned to
--     multi-replica RollingUpdate needs the prd procedure below.
--
--     PRD (rolling multica-api, maxUnavailable:0) overlapped old and new pods.
--     A one-time MULTICA_LARK_HUB_DISABLED park-switch existed during the
--     cutover to hold a hub dormant while the API stayed up, so only one hub
--     was ever live (invariant b). That cutover is complete and the switch has
--     since been removed (MUL-3515); this note is kept as history. Rollback to
--     a pre-cutover build is not lossless once the new hub has written Feishu
--     state into channel_*.
--
-- app_secret_encrypted is BYTEA; it is carried into the JSONB config as a
-- base64 string. PostgreSQL's encode(...,'base64') MIME-wraps the output
-- with a newline every 76 chars, and a secretbox-sealed app secret (~72
-- bytes) exceeds that, so we strip the newlines: Go's encoding/json decodes
-- a base64 string into a []byte field with base64.StdEncoding, which rejects
-- embedded newlines. Stripping keeps the bytea -> JSON -> []byte round-trip
-- symmetric (the Go writer emits unwrapped base64 too) and the ciphertext is
-- never stored in plaintext.

-- =====================
-- channel_installation
-- =====================
CREATE TABLE channel_installation (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL,
    agent_id              UUID NOT NULL,
    channel_type          TEXT NOT NULL,
    -- Platform-specific identifiers/config. For feishu:
    --   app_id, app_secret_encrypted (base64), tenant_key, bot_open_id,
    --   bot_union_id, region.
    config                JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    ws_lease_token        TEXT,
    ws_lease_expires_at   TIMESTAMPTZ,
    installer_user_id     UUID NOT NULL,
    installed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- One installation per (agent, channel_type): an agent may connect more
    -- than one IM at once (feishu + slack + ...), but only one of each kind.
    -- The old lark_installation had UNIQUE(workspace_id, agent_id) because
    -- feishu was the only channel; with a channel_type discriminator the
    -- natural generalization adds it to the key. In the current feishu-only
    -- world this is behaviorally identical (one row per agent). If the
    -- product later wants "one agent, at most one IM regardless of type",
    -- that is an application-layer rule (MUL-3515 §4), not a DB constraint.
    UNIQUE (workspace_id, agent_id, channel_type)
);

CREATE INDEX idx_channel_installation_workspace ON channel_installation(workspace_id);
CREATE INDEX idx_channel_installation_agent ON channel_installation(agent_id);
CREATE INDEX idx_channel_installation_lease ON channel_installation(ws_lease_expires_at)
    WHERE status = 'active';
-- Routing key. Inbound events carry only the platform app identifier
-- (Feishu app_id); the dispatcher routes on (channel_type, app_id). The
-- functional unique index replaces the old global UNIQUE(app_id) and is
-- scoped per channel_type. Rows without an app_id (a future channel that
-- routes differently) store JSON null here, and Postgres allows many
-- NULLs in a unique index, so they do not collide.
CREATE UNIQUE INDEX idx_channel_installation_type_appid
    ON channel_installation(channel_type, (config ->> 'app_id'));

INSERT INTO channel_installation (
    id, workspace_id, agent_id, channel_type, config, status,
    ws_lease_token, ws_lease_expires_at, installer_user_id,
    installed_at, created_at, updated_at
)
SELECT
    id, workspace_id, agent_id, 'feishu',
    jsonb_strip_nulls(jsonb_build_object(
        'app_id',               app_id,
        'app_secret_encrypted', replace(encode(app_secret_encrypted, 'base64'), E'\n', ''),
        'tenant_key',           tenant_key,
        'bot_open_id',          bot_open_id,
        'bot_union_id',         bot_union_id,
        'region',               region
    )),
    status, ws_lease_token, ws_lease_expires_at, installer_user_id,
    installed_at, created_at, updated_at
FROM lark_installation;

-- =====================
-- channel_user_binding
-- =====================
-- channel_user_id is the platform-native, per-installation user id
-- (Feishu open_id). union_id and any other secondary identity goes in
-- config. The member-FK that used to make a row's existence proof of
-- workspace membership is gone; the cutover PR validates membership in
-- the identity check and prunes bindings on member removal.
CREATE TABLE channel_user_binding (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    multica_user_id  UUID NOT NULL,
    installation_id  UUID NOT NULL,
    channel_type     TEXT NOT NULL,
    channel_user_id  TEXT NOT NULL,
    config           JSONB NOT NULL DEFAULT '{}'::jsonb,
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, channel_user_id)
);

CREATE INDEX idx_channel_user_binding_user
    ON channel_user_binding(multica_user_id, workspace_id);
CREATE INDEX idx_channel_user_binding_workspace_user
    ON channel_user_binding(workspace_id, channel_user_id);

INSERT INTO channel_user_binding (
    id, workspace_id, multica_user_id, installation_id,
    channel_type, channel_user_id, config, bound_at
)
SELECT
    id, workspace_id, multica_user_id, installation_id,
    'feishu', lark_open_id,
    jsonb_strip_nulls(jsonb_build_object('union_id', union_id)),
    bound_at
FROM lark_user_binding;

-- =====================
-- channel_chat_session_binding
-- =====================
CREATE TABLE channel_chat_session_binding (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id   UUID NOT NULL,
    installation_id   UUID NOT NULL,
    channel_type      TEXT NOT NULL,
    channel_chat_id   TEXT NOT NULL,
    chat_type         TEXT NOT NULL
        CHECK (chat_type IN ('p2p', 'group')),
    -- Most-recent inbound trigger, so the decoupled outbound patcher can
    -- thread its reply back into the originating topic. Nullable; a NULL
    -- thread id keeps the chat-level send path.
    last_message_id   TEXT,
    last_thread_id    TEXT,
    config            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, channel_chat_id),
    UNIQUE (chat_session_id)
);

CREATE INDEX idx_channel_chat_session_binding_session
    ON channel_chat_session_binding(chat_session_id);

INSERT INTO channel_chat_session_binding (
    id, chat_session_id, installation_id, channel_type,
    channel_chat_id, chat_type, last_message_id, last_thread_id, created_at
)
SELECT
    id, chat_session_id, installation_id, 'feishu',
    lark_chat_id, lark_chat_type, last_lark_message_id, last_lark_thread_id, created_at
FROM lark_chat_session_binding;

-- =====================
-- channel_inbound_message_dedup
-- =====================
-- Two-phase idempotency with owner fencing, unchanged in shape from
-- lark_inbound_message_dedup (keyed per installation + message). Transient
-- 24h cache; copied forward for completeness.
CREATE TABLE channel_inbound_message_dedup (
    installation_id  UUID NOT NULL,
    message_id       TEXT NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    claim_token      UUID NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (installation_id, message_id)
);

CREATE INDEX idx_channel_inbound_dedup_received
    ON channel_inbound_message_dedup(received_at);

INSERT INTO channel_inbound_message_dedup (
    installation_id, message_id, received_at, processed_at, claim_token
)
SELECT installation_id, message_id, received_at, processed_at, claim_token
FROM lark_inbound_message_dedup;

-- =====================
-- channel_inbound_audit
-- =====================
-- Non-content drop audit. installation_id is nullable (the old ON DELETE
-- SET NULL is now just a nullable column the app may leave NULL).
CREATE TABLE channel_inbound_audit (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id     UUID,
    channel_type        TEXT NOT NULL,
    channel_chat_id     TEXT,
    event_type          TEXT NOT NULL,
    channel_event_id    TEXT,
    channel_message_id  TEXT,
    drop_reason         TEXT NOT NULL,
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_inbound_audit_installation
    ON channel_inbound_audit(installation_id, received_at DESC);
CREATE INDEX idx_channel_inbound_audit_reason
    ON channel_inbound_audit(drop_reason, received_at DESC);

INSERT INTO channel_inbound_audit (
    id, installation_id, channel_type, channel_chat_id, event_type,
    channel_event_id, channel_message_id, drop_reason, received_at
)
SELECT
    id, installation_id, 'feishu', lark_chat_id, event_type,
    lark_event_id, lark_message_id, drop_reason, received_at
FROM lark_inbound_audit;

-- =====================
-- channel_outbound_card_message
-- =====================
CREATE TABLE channel_outbound_card_message (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id          UUID NOT NULL,
    task_id                  UUID,
    channel_type             TEXT NOT NULL,
    channel_chat_id          TEXT NOT NULL,
    channel_card_message_id  TEXT NOT NULL,
    status                   TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'streaming', 'final', 'error')),
    last_patched_at          TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_channel_outbound_card_task
    ON channel_outbound_card_message(task_id)
    WHERE task_id IS NOT NULL;
CREATE INDEX idx_channel_outbound_card_session
    ON channel_outbound_card_message(chat_session_id, created_at DESC);

INSERT INTO channel_outbound_card_message (
    id, chat_session_id, task_id, channel_type, channel_chat_id,
    channel_card_message_id, status, last_patched_at, created_at
)
SELECT
    id, chat_session_id, task_id, 'feishu', lark_chat_id,
    lark_card_message_id, status, last_patched_at, created_at
FROM lark_outbound_card_message;

-- =====================
-- channel_binding_token
-- =====================
CREATE TABLE channel_binding_token (
    token_hash       TEXT PRIMARY KEY,
    workspace_id     UUID NOT NULL,
    installation_id  UUID NOT NULL,
    channel_type     TEXT NOT NULL,
    channel_user_id  TEXT NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    consumed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Keep the product TTL cap in lockstep with channel.BindingTokenTTL
    -- (15 minutes), same as the old lark_binding_token CHECK.
    CONSTRAINT channel_binding_token_ttl_cap
        CHECK (expires_at <= created_at + INTERVAL '15 minutes')
);

CREATE INDEX idx_channel_binding_token_installation
    ON channel_binding_token(installation_id, expires_at);

INSERT INTO channel_binding_token (
    token_hash, workspace_id, installation_id, channel_type,
    channel_user_id, expires_at, consumed_at, created_at
)
SELECT
    token_hash, workspace_id, installation_id, 'feishu',
    lark_open_id, expires_at, consumed_at, created_at
FROM lark_binding_token;
