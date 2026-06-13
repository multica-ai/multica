-- WeCom (企业微信) intelligent robot integration: per-agent bot
-- installations, user/chat bindings, inbound dedup + audit, outbound
-- stream context. Mirrors the Lark integration boundaries documented
-- in server/internal/integrations/wecom/doc.go.

CREATE TABLE wecom_installation (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id            UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id                UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    bot_id                  TEXT NOT NULL,
    bot_secret_encrypted    BYTEA NOT NULL,
    corp_id                 TEXT NOT NULL,
    corp_secret_encrypted   BYTEA NOT NULL,
    self_build_agent_id     TEXT,
    installer_user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    status                  TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    ws_lease_token          TEXT,
    ws_lease_expires_at     TIMESTAMPTZ,
    installed_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id),
    UNIQUE (bot_id),
    UNIQUE (id, workspace_id)
);

CREATE INDEX idx_wecom_installation_workspace ON wecom_installation(workspace_id);
CREATE INDEX idx_wecom_installation_agent ON wecom_installation(agent_id);
CREATE INDEX idx_wecom_installation_lease ON wecom_installation(ws_lease_expires_at)
    WHERE status = 'active';

CREATE TABLE wecom_user_binding (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    multica_user_id      UUID NOT NULL,
    installation_id      UUID NOT NULL,
    wecom_userid         TEXT NOT NULL,
    wecom_open_userid    TEXT,
    bound_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, wecom_userid),
    CONSTRAINT wecom_user_binding_installation_fk
        FOREIGN KEY (installation_id, workspace_id)
        REFERENCES wecom_installation(id, workspace_id)
        ON DELETE CASCADE,
    CONSTRAINT wecom_user_binding_member_fk
        FOREIGN KEY (workspace_id, multica_user_id)
        REFERENCES member(workspace_id, user_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_wecom_user_binding_user
    ON wecom_user_binding(multica_user_id, workspace_id);

CREATE TABLE wecom_chat_session_binding (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_session_id   UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    installation_id   UUID NOT NULL REFERENCES wecom_installation(id) ON DELETE CASCADE,
    wecom_chat_id     TEXT NOT NULL,
    wecom_chat_type   TEXT NOT NULL
        CHECK (wecom_chat_type IN ('single', 'group')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (installation_id, wecom_chat_id),
    UNIQUE (chat_session_id)
);

CREATE INDEX idx_wecom_chat_session_binding_session
    ON wecom_chat_session_binding(chat_session_id);

CREATE TABLE wecom_inbound_message_dedup (
    installation_id  UUID NOT NULL REFERENCES wecom_installation(id) ON DELETE CASCADE,
    message_id       TEXT NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    claim_token      UUID NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (installation_id, message_id)
);

CREATE INDEX idx_wecom_inbound_dedup_received
    ON wecom_inbound_message_dedup(received_at);

CREATE TABLE wecom_inbound_audit (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID REFERENCES wecom_installation(id) ON DELETE SET NULL,
    wecom_chat_id     TEXT,
    event_type        TEXT NOT NULL,
    wecom_message_id  TEXT,
    drop_reason       TEXT NOT NULL,
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wecom_inbound_audit_installation
    ON wecom_inbound_audit(installation_id, received_at DESC);

CREATE TABLE wecom_outbound_stream (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id   UUID NOT NULL REFERENCES wecom_installation(id) ON DELETE CASCADE,
    chat_session_id   UUID NOT NULL REFERENCES chat_session(id) ON DELETE CASCADE,
    task_id           UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    req_id            TEXT NOT NULL,
    stream_id         TEXT NOT NULL,
    wecom_chat_id     TEXT NOT NULL,
    wecom_chat_type   TEXT NOT NULL
        CHECK (wecom_chat_type IN ('single', 'group')),
    status            TEXT NOT NULL DEFAULT 'streaming'
        CHECK (status IN ('streaming', 'final', 'error')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_wecom_outbound_stream_task
    ON wecom_outbound_stream(task_id)
    WHERE task_id IS NOT NULL;

CREATE TABLE wecom_binding_token (
    token_hash       TEXT PRIMARY KEY,
    workspace_id     UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id  UUID NOT NULL REFERENCES wecom_installation(id) ON DELETE CASCADE,
    wecom_userid     TEXT NOT NULL,
    expires_at       TIMESTAMPTZ NOT NULL,
    consumed_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT wecom_binding_token_ttl_cap
        CHECK (expires_at <= created_at + INTERVAL '15 minutes')
);

CREATE INDEX idx_wecom_binding_token_installation
    ON wecom_binding_token(installation_id, expires_at);
