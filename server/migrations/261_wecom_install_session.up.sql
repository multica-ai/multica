CREATE TABLE wecom_install_session (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_key_hash        TEXT NOT NULL,
    workspace_id            UUID NOT NULL,
    agent_id                UUID NOT NULL,
    initiator_user_id       UUID NOT NULL,
    scode_encrypted         TEXT,
    qr_code_url_encrypted   TEXT,
    status                  TEXT NOT NULL DEFAULT 'creating'
        CHECK (status IN ('creating', 'pending', 'success', 'error')),
    poll_after              TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at              TIMESTAMPTZ,
    lease_token             TEXT,
    lease_expires_at        TIMESTAMPTZ,
    installation_id         UUID,
    error_reason            TEXT,
    error_message           TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
