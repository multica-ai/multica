CREATE TABLE workspace_superuser_policy (
    workspace_id UUID PRIMARY KEY,
    enabled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    enabled_by UUID NOT NULL
);

CREATE TABLE workspace_superuser_grant (
    member_id UUID PRIMARY KEY,
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    granted_by UUID NOT NULL,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspace_superuser_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    actor_user_id UUID NOT NULL,
    target_user_id UUID,
    chat_session_id UUID,
    action TEXT NOT NULL CHECK (action IN ('grant', 'revoke', 'read_chat')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
