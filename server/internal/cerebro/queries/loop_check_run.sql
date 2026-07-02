-- FIR-2283 loop check transport. These queries are the persistence half of the
-- delivery gate: the engine enqueues the checks a loop round requires, the
-- runtime reports each exit code, and the engine lists them back to feed
-- loops.Reconcile / loops.EvaluateGate. Scoped per (issue, gate, round).

-- name: EnqueueLoopCheckRun :exec
-- Register a pending check (ran = false) for a gate round. Idempotent: a second
-- enqueue of the same argv leaves the existing row — and any reported result on
-- it — untouched, so a re-fire never resets a check that already ran.
INSERT INTO cerebro_loop_check_run (issue_id, gate, round, argv)
VALUES (@issue_id, @gate, @round, @argv)
ON CONFLICT (issue_id, gate, round, argv) DO NOTHING;

-- name: ReportLoopCheckRun :exec
-- Record the runtime's reported exit code for one enqueued check.
UPDATE cerebro_loop_check_run
SET ran = true, exit_code = @exit_code, updated_at = now()
WHERE issue_id = @issue_id AND gate = @gate AND round = @round AND argv = @argv;

-- name: ListLoopCheckRuns :many
-- Every check (pending and reported) for a gate round, oldest first, so the
-- engine can fold the rows into the []CheckOutcome the gate logic consumes.
SELECT * FROM cerebro_loop_check_run
WHERE issue_id = @issue_id AND gate = @gate AND round = @round
ORDER BY created_at ASC, id ASC;
