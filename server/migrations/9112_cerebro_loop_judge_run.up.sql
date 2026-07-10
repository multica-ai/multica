-- FIR-2283 loop check transport (judge checks). Mirrors
-- cerebro_loop_check_run (migration 9109/9110), but for judge-type
-- verification: instead of an argv command and an exit code, a judge check
-- carries the rubric it was dispatched with and the structured verdict a
-- judge agent reports back. A row is created pending (ran = false) when the
-- engine enqueues a judge check, then updated in place with the verdict when
-- the judge reports. The judge's free-form reasoning is never stored — only
-- the pass/fail verdict and the specific blocking issues it names — which is
-- what makes the gate's decision auditable the same way a check's exit code
-- is.
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
    -- One row per judge check id within a gate round; enqueue is idempotent
    -- and a duplicate report updates the same row instead of inserting a
    -- second.
    UNIQUE (issue_id, gate, round, check_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_judge_run_gate
    ON cerebro_loop_judge_run(issue_id, gate, round);
