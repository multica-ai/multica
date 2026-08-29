CREATE TABLE agent_tool_approval_request (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    task_id UUID NOT NULL,
    issue_id UUID,
    chat_session_id UUID,
    invocation_id UUID NOT NULL,
    idempotency_key TEXT NOT NULL CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    transport_kind TEXT NOT NULL CHECK (transport_kind IN ('managed_mcp', 'managed_native')),
    server_key TEXT NOT NULL CHECK (char_length(server_key) BETWEEN 1 AND 255),
    tool_name TEXT NOT NULL CHECK (char_length(tool_name) BETWEEN 1 AND 255),
    schema_digest TEXT NOT NULL CHECK (schema_digest ~ '^sha256:[0-9a-f]{64}$'),
    policy_revision BIGINT NOT NULL CHECK (policy_revision > 0),
    schema_field_names TEXT[] NOT NULL DEFAULT '{}'::text[],
    argument_bytes INTEGER NOT NULL DEFAULT 0 CHECK (argument_bytes BETWEEN 0 AND 134217728),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'consumed', 'denied', 'expired', 'cancelled')),
    reason_code TEXT CHECK (
        reason_code IN (
            'operator_approved', 'operator_denied', 'unexpected_action',
            'risk_too_high', 'not_needed', 'policy_replaced',
            'task_cancelled', 'request_expired', 'agent_cleanup',
            'workspace_cleanup'
        )
    ),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    decided_by_user_id UUID,
    CHECK (idempotency_key = btrim(idempotency_key)),
    CHECK (server_key = btrim(server_key)),
    CHECK (tool_name = btrim(tool_name)),
    CHECK (cardinality(schema_field_names) <= 128),
    CHECK (array_position(schema_field_names, NULL) IS NULL),
    CHECK (expires_at > requested_at),
    CHECK (expires_at <= requested_at + interval '24 hours'),
    CHECK (
        (status = 'pending'
            AND decided_at IS NULL
            AND consumed_at IS NULL
            AND decided_by_user_id IS NULL
            AND reason_code IS NULL)
        OR (status = 'approved'
            AND decided_at IS NOT NULL
            AND consumed_at IS NULL
            AND decided_by_user_id IS NOT NULL
            AND reason_code = 'operator_approved')
        OR (status = 'consumed'
            AND decided_at IS NOT NULL
            AND consumed_at IS NOT NULL
            AND consumed_at >= decided_at
            AND decided_by_user_id IS NOT NULL
            AND reason_code = 'operator_approved')
        OR (status = 'denied'
            AND decided_at IS NOT NULL
            AND consumed_at IS NULL
            AND decided_by_user_id IS NOT NULL
            AND reason_code IN ('operator_denied', 'unexpected_action', 'risk_too_high', 'not_needed'))
        OR (status = 'expired'
            AND decided_at IS NOT NULL
            AND consumed_at IS NULL
            AND reason_code = 'request_expired')
        OR (status = 'cancelled'
            AND decided_at IS NOT NULL
            AND consumed_at IS NULL
            AND reason_code IN ('policy_replaced', 'task_cancelled', 'agent_cleanup', 'workspace_cleanup'))
    )
);
