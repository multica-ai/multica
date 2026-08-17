CREATE TABLE dingtalk_workspace_grant (
    id                UUID NOT NULL DEFAULT gen_random_uuid(),
    connector_id      UUID NOT NULL,
    workspace_id      UUID NOT NULL,
    default_agent_id  UUID NOT NULL,
    installer_user_id UUID NOT NULL,
    status            TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'revoked')),
    installed_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
