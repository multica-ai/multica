-- Durable compensation/saga row (Plan v1.2 section 11). No inline unique
-- constraint; idempotency uniqueness arrives via index migration.
CREATE TABLE memoryhub_compensation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    binding_id UUID,
    op TEXT NOT NULL CHECK (op IN ('create_remote', 'reuse_remote', 'rebind_remote', 'delete_remote', 'purge_memory')),
    idempotency_key TEXT NOT NULL,
    remote_ref JSONB NOT NULL DEFAULT '{}'::jsonb,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'retry_wait', 'compensated', 'blocked', 'dead_letter')),
    attempt INTEGER NOT NULL DEFAULT 0,
    max_attempt INTEGER NOT NULL DEFAULT 6,
    next_attempt_at TIMESTAMPTZ,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL DEFAULT '',
    evidence_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
