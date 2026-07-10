-- FIR-2283 loop judge check transport sqlc schema input. Mirrors
-- server/migrations/9112_cerebro_loop_judge_run.up.sql so sqlc generates
-- type-safe Go against cerebro_loop_judge_run.
CREATE TABLE IF NOT EXISTS cerebro_loop_judge_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    round INT NOT NULL,
    check_id TEXT NOT NULL,
    rubric TEXT NOT NULL,
    ran BOOLEAN NOT NULL DEFAULT false,
    pass BOOLEAN NOT NULL DEFAULT false,
    blocking_issues TEXT[] NOT NULL DEFAULT '{}',
    dispatched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, gate, round, check_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_judge_run_gate
    ON cerebro_loop_judge_run(issue_id, gate, round);
