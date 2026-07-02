-- FIR-2283 loop check transport (human checks). Mirrors
-- cerebro_loop_judge_run (migration 9112), but for human-type verification:
-- instead of a model scoring a rubric, a person (or an agent standing in for
-- one) explicitly approves or rejects. A row is created pending (ran = false)
-- when the engine enqueues a human check, then updated in place when the
-- assignee reports a decision — via report_loop_human (agent assignee) or the
-- approve-human-check HTTP endpoint (person assignee).
CREATE TABLE IF NOT EXISTS cerebro_loop_human_approval (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    round INT NOT NULL,
    check_id TEXT NOT NULL,
    prompt TEXT NOT NULL DEFAULT '',
    -- assignee_type is 'agent' or 'member' (workflows.AssigneeTypeAgent /
    -- AssigneeTypeMember) — who must sign off.
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
    -- One row per check id within a gate round; enqueue is idempotent and a
    -- duplicate report updates the same row instead of inserting a second.
    UNIQUE (issue_id, gate, round, check_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_human_approval_gate
    ON cerebro_loop_human_approval(issue_id, gate, round);
