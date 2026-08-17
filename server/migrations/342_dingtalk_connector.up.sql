CREATE TABLE dingtalk_connector (
    id                  UUID NOT NULL DEFAULT gen_random_uuid(),
    app_id              TEXT NOT NULL,
    config              JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    ws_lease_token      TEXT,
    ws_lease_expires_at TIMESTAMPTZ,
    installer_user_id   UUID NOT NULL,
    installed_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
