CREATE TABLE agent_tool_policy_revision (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'active')),
    policy_digest TEXT NOT NULL CHECK (policy_digest ~ '^sha256:[0-9a-f]{64}$'),
    default_effect TEXT NOT NULL DEFAULT 'deny' CHECK (default_effect = 'deny'),
    rule_identities JSONB NOT NULL DEFAULT '[]'::jsonb,
    actor_user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (jsonb_typeof(rule_identities) = 'array'),
    CHECK (octet_length(rule_identities::text) <= 1048576)
);
