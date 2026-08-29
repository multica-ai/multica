CREATE TABLE agent_tool_action_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    task_id UUID NOT NULL,
    issue_id UUID,
    invocation_id UUID NOT NULL,
    approval_request_id UUID,
    transport_kind TEXT NOT NULL CHECK (transport_kind IN ('managed_mcp', 'managed_native')),
    server_key TEXT NOT NULL CHECK (char_length(server_key) BETWEEN 1 AND 255),
    tool_name TEXT NOT NULL CHECK (char_length(tool_name) BETWEEN 1 AND 255),
    schema_digest TEXT NOT NULL CHECK (schema_digest ~ '^sha256:[0-9a-f]{64}$'),
    coverage_kind TEXT NOT NULL
        CHECK (coverage_kind IN ('managed_mcp', 'managed_native', 'declaration_only')),
    event_type TEXT NOT NULL CHECK (
        event_type IN (
            'requested', 'policy_allowed', 'policy_denied',
            'approval_requested', 'approval_approved', 'approval_denied',
            'approval_expired', 'approval_consumed', 'started',
            'succeeded', 'failed', 'cancelled'
        )
    ),
    argument_bytes INTEGER CHECK (argument_bytes BETWEEN 0 AND 134217728),
    result_bytes INTEGER CHECK (result_bytes BETWEEN 0 AND 134217728),
    duration_ms BIGINT CHECK (duration_ms BETWEEN 0 AND 86400000),
    outcome_code TEXT CHECK (
        outcome_code IN (
            'allowed', 'denied', 'approval_required', 'approved',
            'consumed', 'expired', 'cancelled', 'started',
            'succeeded', 'failed'
        )
    ),
    error_class TEXT CHECK (
        error_class IN (
            'transport', 'timeout', 'cancelled', 'invalid_request',
            'provider', 'internal', 'audit', 'schema_drift',
            'unsupported', 'policy'
        )
    ),
    actor_user_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (server_key = btrim(server_key)),
    CHECK (tool_name = btrim(tool_name)),
    CHECK (
        (event_type IN ('approval_approved', 'approval_denied') AND actor_user_id IS NOT NULL)
        OR (event_type NOT IN ('approval_approved', 'approval_denied') AND actor_user_id IS NULL)
    )
);
