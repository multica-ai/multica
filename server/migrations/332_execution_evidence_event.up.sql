-- H6 ordered evidence event row (v1.3 A5.1). typed payload_ref as JSONB plus
-- payload_sha256; retention drives pruning. No inline indexes.
CREATE TABLE execution_evidence_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    schema_version INTEGER NOT NULL DEFAULT 1,
    execution_id UUID NOT NULL,
    run_id TEXT NOT NULL,
    workspace_id UUID NOT NULL,
    project_id UUID,
    agent_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    model TEXT NOT NULL,
    sequence BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('output', 'message', 'usage', 'artifact', 'test', 'reviewer', 'completion', 'gate_failure')),
    payload_ref JSONB NOT NULL,
    payload_sha256 TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    retention_until TIMESTAMPTZ NOT NULL
);
