-- WeChat Work (企业微信) intelligent Bot long-connection integration.
-- Parallels lark_integration (migration 109) but is structurally simpler:
-- no device-flow, no per-app open_id (WeCom identifies users by userid),
-- no outbound card-patching (WeCom uses stream replies instead of card
-- updates). The bot_id + secret pair is directly configured by the user.

-- =====================
-- wechat_installation
-- =====================
-- One row per (workspace, agent). Each Multica Agent owns at most one
-- WeCom intelligent bot. `secret_encrypted` is ciphertext produced by
-- the application-layer secretbox helper.
CREATE TABLE wechat_installation (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id              UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    bot_id                TEXT NOT NULL,
    -- Ciphertext of the WeCom bot secret. Application-layer secretbox.
    secret_encrypted      BYTEA NOT NULL,
    installer_user_id     UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    status                TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    -- WS ownership lease: same pattern as lark_installation.
    ws_lease_token        TEXT,
    ws_lease_expires_at   TIMESTAMPTZ,
    installed_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id),
    UNIQUE (bot_id),
    UNIQUE (id, workspace_id)
);

CREATE INDEX idx_wechat_installation_workspace ON wechat_installation(workspace_id);
CREATE INDEX idx_wechat_installation_agent ON wechat_installation(agent_id);
CREATE INDEX idx_wechat_installation_lease ON wechat_installation(ws_lease_expires_at)
    WHERE status = 'active';

-- =====================
-- wechat_user_binding
-- =====================
-- Maps a WeCom `userid` to a Multica user, per-installation.
-- WeCom userids are workspace-scoped (corp-level), but we still key on
-- (installation_id, wechat_userid) for multi-corp isolation.
CREATE TABLE wechat_user_binding (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    multica_user_id  UUID NOT NULL,
    installation_id  UUID NOT NULL,
    wechat_userid    TEXT NOT NULL,
    bound_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, wechat_userid),
    CONSTRAINT wechat_user_binding_installation_fk
        FOREIGN KEY (installation_id, workspace_id)
        REFERENCES wechat_installation(id, workspace_id)
        ON DELETE CASCADE,
    CONSTRAINT wechat_user_binding_member_fk
        FOREIGN KEY (workspace_id, multica_user_id)
        REFERENCES member(workspace_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_wechat_user_binding_user
    ON wechat_user_binding(multica_user_id, workspace_id);
CREATE INDEX idx_wechat_user_binding_workspace_userid
    ON wechat_user_binding(workspace_id, wechat_userid);

-- =====================
-- wechat_chat_session_binding
-- =====================
-- One WeCom chat (chat_id or p2p pseudo-key) ↔ one Multica chat_session.
CREATE TABLE wechat_chat_session_binding (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id   UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    installation_id   UUID NOT NULL REFERENCES wechat_installation(id) ON DELETE CASCADE,
    wechat_chat_id    TEXT NOT NULL,
    wechat_chat_type  TEXT NOT NULL
        CHECK (wechat_chat_type IN ('single', 'group')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, wechat_chat_id),
    UNIQUE (chat_session_id)
);

CREATE INDEX idx_wechat_chat_session_binding_session
    ON wechat_chat_session_binding(chat_session_id);

-- =====================
-- wechat_inbound_message_dedup
-- =====================
-- Idempotency for WeCom inbound events. Same two-phase claim semantics
-- as lark_inbound_message_dedup.
CREATE TABLE wechat_inbound_message_dedup (
    message_id    TEXT PRIMARY KEY,
    installation_id UUID NOT NULL REFERENCES wechat_installation(id) ON DELETE CASCADE,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ,
    claim_token   UUID NOT NULL DEFAULT gen_random_uuid()
);

CREATE INDEX idx_wechat_inbound_dedup_received
    ON wechat_inbound_message_dedup(received_at);

-- =====================
-- wechat_inbound_audit
-- =====================
-- Non-content audit log for dropped events. Same purpose as lark_inbound_audit.
CREATE TABLE wechat_inbound_audit (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID REFERENCES wechat_installation(id) ON DELETE SET NULL,
    wechat_chat_id    TEXT,
    event_type        TEXT NOT NULL,
    wechat_message_id TEXT,
    drop_reason       TEXT NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wechat_inbound_audit_installation
    ON wechat_inbound_audit(installation_id, received_at DESC);
CREATE INDEX idx_wechat_inbound_audit_reason
    ON wechat_inbound_audit(drop_reason, received_at DESC);
