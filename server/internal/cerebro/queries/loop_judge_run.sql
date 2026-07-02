-- FIR-2283 loop judge check transport. Mirrors loop_check_run.sql, but for
-- judge-type verification: the engine enqueues judge checks a loop round
-- requires, a judge agent reports each verdict, and the engine lists them
-- back to feed loops.Reconcile. Scoped per (issue, gate, round).

-- name: EnqueueLoopJudgeRun :exec
-- Register a pending judge check (ran = false) for a gate round. Idempotent:
-- a second enqueue of the same check_id leaves the existing row — and any
-- reported verdict on it — untouched.
INSERT INTO cerebro_loop_judge_run (issue_id, gate, round, check_id, rubric)
VALUES (@issue_id, @gate, @round, @check_id, @rubric)
ON CONFLICT (issue_id, gate, round, check_id) DO NOTHING;

-- name: ReportLoopJudgeRun :exec
-- Record the judge agent's reported verdict for one enqueued judge check.
UPDATE cerebro_loop_judge_run
SET ran = true, pass = @pass, blocking_issues = COALESCE(@blocking_issues, '{}'), updated_at = now()
WHERE issue_id = @issue_id AND gate = @gate AND round = @round AND check_id = @check_id;

-- name: ListLoopJudgeRuns :many
-- Every judge check (pending and reported) for a gate round, oldest first, so
-- the engine can fold the rows into the []JudgeOutcome the gate logic
-- consumes.
SELECT * FROM cerebro_loop_judge_run
WHERE issue_id = @issue_id AND gate = @gate AND round = @round
ORDER BY created_at ASC, id ASC;
