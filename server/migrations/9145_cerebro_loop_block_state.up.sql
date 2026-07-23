-- FIR-3493 block-chain runtime state. The chain driver records one durable
-- phase state per issue/workflow/phase and one row per runtime block step.
-- Limits are independent and terminal: once a phase is failed, retries cannot
-- reopen it or dispatch more work.
CREATE TABLE IF NOT EXISTS cerebro_loop_phase_state (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workflow_id UUID NOT NULL REFERENCES cerebro_workflow(id) ON DELETE CASCADE,
    phase_id TEXT NOT NULL CHECK (phase_id <> ''),
    steps_opened INT NOT NULL DEFAULT 0 CHECK (steps_opened >= 0),
    rounds_used INT NOT NULL DEFAULT 0 CHECK (rounds_used >= 0),
    consecutive_stalls INT NOT NULL DEFAULT 0 CHECK (consecutive_stalls >= 0),
    last_outcome_signature TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed')),
    failure_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, workflow_id, phase_id)
);

CREATE TABLE IF NOT EXISTS cerebro_loop_step (
    issue_id UUID NOT NULL,
    workflow_id UUID NOT NULL,
    phase_id TEXT NOT NULL CHECK (phase_id <> ''),
    block_id TEXT NOT NULL CHECK (block_id <> ''),
    step_number INT NOT NULL CHECK (step_number > 0),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'waiting', 'completed', 'failed')),
    outcome JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, workflow_id, phase_id, block_id, step_number),
    FOREIGN KEY (issue_id, workflow_id, phase_id)
        REFERENCES cerebro_loop_phase_state(issue_id, workflow_id, phase_id)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_step_phase_created
    ON cerebro_loop_step(issue_id, workflow_id, phase_id, created_at);
