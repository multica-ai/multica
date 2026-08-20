-- Durable execution-side consequence of an authenticated VIBES restriction.
-- This is receipt/retry evidence, never a Workspace or Membership authority.
CREATE TABLE tag_member_execution_cleanup (
    source TEXT NOT NULL,
    delivery_id TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    vibes_workspace_id TEXT,
    vibes_user_id TEXT,
    authority_version BIGINT,
    identity_restriction_version BIGINT,
    account_epoch BIGINT,
    payload_digest BYTEA NOT NULL,
    target_digest BYTEA NOT NULL,
    targets JSONB NOT NULL DEFAULT '[]'::jsonb,
    state TEXT NOT NULL DEFAULT 'requested',
    outcome TEXT NOT NULL DEFAULT '',
    attempt_count INTEGER NOT NULL DEFAULT 1,
    failure_code TEXT NOT NULL DEFAULT '',
    effects JSONB NOT NULL DEFAULT '{}'::jsonb,
    receipt_id TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_retry_at TIMESTAMPTZ,
    failed_at TIMESTAMPTZ,
    applied_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_member_cleanup_source_check CHECK (source IN ('workspace_projection', 'identity_restriction')),
    CONSTRAINT tag_member_cleanup_state_check CHECK (state IN ('requested', 'retry', 'failed', 'applied')),
    CONSTRAINT tag_member_cleanup_attempt_check CHECK (attempt_count > 0),
    CONSTRAINT tag_member_cleanup_payload_digest_check CHECK (octet_length(payload_digest) = 32),
    CONSTRAINT tag_member_cleanup_target_digest_check CHECK (octet_length(target_digest) = 32),
    CONSTRAINT tag_member_cleanup_workspace_shape_check CHECK (
        (source = 'workspace_projection' AND vibes_workspace_id IS NOT NULL AND vibes_user_id IS NULL
            AND authority_version IS NOT NULL AND identity_restriction_version IS NULL AND account_epoch IS NULL)
        OR
        (source = 'identity_restriction' AND vibes_workspace_id IS NULL AND vibes_user_id IS NOT NULL
            AND authority_version IS NULL AND identity_restriction_version IS NOT NULL AND account_epoch IS NOT NULL)
    ),
    CONSTRAINT tag_member_cleanup_completion_check CHECK (
        (state = 'applied' AND receipt_id <> '' AND applied_at IS NOT NULL)
        OR state <> 'applied'
    )
);
