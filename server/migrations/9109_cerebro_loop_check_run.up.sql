-- FIR-2283 loop check transport (persistence). The deterministic loop brain
-- (loops.Reconcile / loops.EvaluateGate) decides a delivery gate purely from
-- the reported check outcomes. This table is where those outcomes live between
-- the moment the engine enqueues a check and the moment the runtime reports its
-- exit code, scoped per (issue, gate, round) so a re-run of the same loop round
-- starts from a clean slate and a later round never sees a stale result.
--
-- A row is created pending (ran = false) when the engine enqueues a check, then
-- updated in place to ran = true with the exit code when the runtime reports.
-- The agent's opinion is never stored — only the argv it was told to run and
-- the exit code it reported — which is what makes the gate trustworthy.
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
    -- One row per check command within a gate round; enqueue is idempotent and a
    -- duplicate report updates the same row instead of inserting a second.
    UNIQUE (issue_id, gate, round, argv)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_loop_check_run_gate
    ON cerebro_loop_check_run(issue_id, gate, round);
