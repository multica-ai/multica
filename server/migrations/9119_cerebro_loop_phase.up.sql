-- FIR-2283 followup point 6 — multi-phase build loops. A loop recipe can now
-- split the build into an ordered chain of build phases, each gated by its own
-- delivery review that must pass before the next phase's build is dispatched.
--
-- The engine distinguishes loop steps by issue status (in_progress / in_review),
-- so "which phase" cannot be read from the status alone. This table gives the
-- gate a per-issue phase pointer, scoped per (issue, gate) exactly like
-- cerebro_loop_gate_state:
--
--   phase — the zero-based index of the build phase currently in flight. Starts
--           at 0 (first phase) and advances by one each time a phase's delivery
--           gate passes and there is another phase to run. The gate evaluator
--           evaluates phase p's checks and, on advance, either dispatches
--           phase p+1's build (intermediate) or lets the gate set Done (last).
--
-- A single-phase loop never writes here (its gate config carries no phases), so
-- this table stays empty for every recipe that doesn't opt into build phases.
CREATE TABLE IF NOT EXISTS cerebro_loop_phase (
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gate TEXT NOT NULL,
    phase INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, gate)
);
