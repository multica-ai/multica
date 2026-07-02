-- FIR-2283 loop check transport sqlc schema input. Mirrors
-- server/migrations/9109_cerebro_loop_check_run.up.sql so sqlc generates
-- type-safe Go against cerebro_loop_check_run.
CREATE TABLE IF NOT EXISTS cerebro_loop_check_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    round INT NOT NULL,
    argv TEXT[] NOT NULL,
    ran BOOLEAN NOT NULL DEFAULT false,
    exit_code INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, gate, round, argv)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_check_run_gate
    ON cerebro_loop_check_run(issue_id, gate, round);
