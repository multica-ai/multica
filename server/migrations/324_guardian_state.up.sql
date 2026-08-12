-- Guardian persistent state: fingerprint, lease, candidate score, handoff,
-- and next-wakeup fields (hard gate 2). No inline indexes.
CREATE TABLE guardian_state (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'running', 'retry_wait', 'handoff_pending', 'blocked', 'dead_letter')),
    fingerprint TEXT NOT NULL,
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    next_wakeup TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    score INTEGER,
    handoff_of UUID,
    candidate_agent_id UUID,
    candidate_runtime_id UUID,
    evidence_ref TEXT,
    failure_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
