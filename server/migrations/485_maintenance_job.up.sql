-- Operator-driven maintenance only; creating this table starts no work.
CREATE TABLE IF NOT EXISTS maintenance_job (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    job_type TEXT NOT NULL,
    job_version INTEGER NOT NULL CHECK (job_version > 0),
    scope_key TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ready', 'paused', 'completed', 'cancelled')),
    revision BIGINT NOT NULL DEFAULT 0,
    dry_run BOOLEAN NOT NULL DEFAULT true,
    options JSONB NOT NULL,
    parameters JSONB NOT NULL DEFAULT '{}',
    checkpoint JSONB NOT NULL DEFAULT '{}',
    progress JSONB NOT NULL DEFAULT '{}',
    result JSONB NOT NULL DEFAULT '{}',
    last_error TEXT NOT NULL DEFAULT '',
    next_allowed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    CHECK (jsonb_typeof(options) = 'object' AND jsonb_typeof(parameters) = 'object'
        AND jsonb_typeof(checkpoint) = 'object' AND jsonb_typeof(progress) = 'object'
        AND jsonb_typeof(result) = 'object')
);
