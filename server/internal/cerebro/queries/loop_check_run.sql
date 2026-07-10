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

-- FIR-2283 loop stop-rules (termination guards). The caps tracker behind
-- loops.Store.LoadGateState / RecordRevision, scoped per (issue, gate) — see
-- cerebro_loop_gate_state (migration 9111).

-- name: LoadLoopGateState :one
-- Create the row on first access (round 1, not stopped), then read it back.
INSERT INTO cerebro_loop_gate_state (issue_id, gate)
VALUES (@issue_id, @gate)
ON CONFLICT (issue_id, gate) DO NOTHING;

SELECT * FROM cerebro_loop_gate_state
WHERE issue_id = @issue_id AND gate = @gate;

-- name: RecordLoopGateRevision :exec
-- Advance a gate to a new round after a GateRevise decision, applying the
-- caps decision computed in Go (loops.Store.RecordRevision) — round,
-- revisions, and consecutive_stalls are already incremented by the caller;
-- this just persists the result.
UPDATE cerebro_loop_gate_state
SET round = @round, revisions = @revisions, consecutive_stalls = @consecutive_stalls,
    last_outcome_signature = @last_outcome_signature, stopped = @stopped,
    stop_reason = @stop_reason, updated_at = now()
WHERE issue_id = @issue_id AND gate = @gate;
