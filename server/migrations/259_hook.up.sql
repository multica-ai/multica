-- Event Hooks MVP — stable hook identity and lifecycle ownership.
-- UUID associations are application-validated; do not add FKs or cascades.
CREATE TABLE hook (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    active_revision_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'issue')),
    scope_id UUID,
    retire_after_event_seq BIGINT,
    origin TEXT NOT NULL DEFAULT 'user' CHECK (origin IN ('user', 'system')),
    system_key TEXT,
    system_version INT,
    creator_actor_type TEXT NOT NULL,
    creator_actor_id UUID,
    authorization_principal_user_id UUID,
    disabled_reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at TIMESTAMPTZ
);

COMMENT ON TABLE hook IS 'Event Hooks stable identity and lifecycle owner. No foreign keys; application validates all associations.';
