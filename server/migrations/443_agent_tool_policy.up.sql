CREATE TABLE agent_tool_policy (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active')),
    policy_digest TEXT NOT NULL CHECK (policy_digest ~ '^sha256:[0-9a-f]{64}$'),
    default_effect TEXT NOT NULL DEFAULT 'deny' CHECK (default_effect = 'deny'),
    created_by_user_id UUID NOT NULL,
    updated_by_user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (updated_at >= created_at)
);
