-- FIR-2283 loop human check transport sqlc schema input. Mirrors
-- server/migrations/9114_cerebro_loop_human_approval.up.sql so sqlc knows
-- the table shape (queries against it stay raw pgx, see loops/store.go).
CREATE TABLE IF NOT EXISTS cerebro_loop_human_approval (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    round INT NOT NULL,
    check_id TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    assignee_type TEXT NOT NULL,
    assignee_id UUID NOT NULL,
    ran BOOLEAN NOT NULL DEFAULT false,
    approved BOOLEAN NOT NULL DEFAULT false,
    note TEXT NOT NULL DEFAULT '',
    approved_by_id UUID,
    approved_by_type TEXT,
    dispatched_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, gate, round, check_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_human_approval_gate
    ON cerebro_loop_human_approval(issue_id, gate, round);
