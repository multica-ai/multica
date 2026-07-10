-- FIR-2283 loop stop-rules (termination guards). Reconcile already returns
-- GateRevise when a required check fails, but until now the engine did
-- nothing with that verdict — a failed check just sat there forever, with no
-- re-dispatch and no cap on how many times it could fail. This table gives
-- the gate a memory across revisions, scoped per (issue, gate) exactly like
-- cerebro_loop_check_run is scoped per (issue, gate, round):
--
--   round               — the loop round currently in flight. Starts at 1 and
--                          advances by one each time the gate revises.
--   revisions           — total number of GateRevise decisions seen. Compared
--                          against Spec.Caps.MaxRevisions.
--   consecutive_stalls  — how many revisions in a row reported the exact same
--                          outcome signature as the one before (no progress).
--                          Compared against Spec.Caps.NoProgressStalls.
--   last_outcome_signature — a stable hash of the previous round's reported
--                          outcomes, used only to detect a stall.
--   stopped / stop_reason — once any cap trips, the gate freezes: it stops
--                          re-dispatching and stops enqueuing new checks. A
--                          human has to intervene (loop:escalate-stalled
--                          already notifies the owner on review entry).
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
