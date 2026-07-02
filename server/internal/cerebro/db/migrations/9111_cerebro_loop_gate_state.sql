-- FIR-2283 loop stop-rules sqlc schema input. Mirrors
-- server/migrations/9111_cerebro_loop_gate_state.up.sql so the schema sqlc
-- reads stays in sync with the runtime migration.
CREATE TABLE IF NOT EXISTS cerebro_loop_gate_state (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    round INT NOT NULL DEFAULT 1,
    revisions INT NOT NULL DEFAULT 0,
    consecutive_stalls INT NOT NULL DEFAULT 0,
    last_outcome_signature TEXT NOT NULL DEFAULT '',
    stopped BOOLEAN NOT NULL DEFAULT false,
    stop_reason TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, gate)
);
