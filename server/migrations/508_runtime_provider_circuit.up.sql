-- Migration 507 (SE-37711 / SE-37664): a runtime-scoped distributed circuit
-- breaker over a runtime's provider.
--
-- When a runtime's provider hits a quota wall (or an auth/access wall) every
-- agent bound to that runtime is affected the same way, so the hold is recorded
-- once per (runtime_id, provider), not per agent. The dispatcher reads this row
-- to decide whether to fall through to the next binding, and the terminal task
-- callbacks write it.
--
-- State machine (serialized on the single row):
--   * closed    — provider healthy, dispatch normally.
--   * open      — provider held until reset_at; selector skips this runtime.
--   * half_open — reset_at has passed; exactly one probe task may test the
--                 provider (probe_task_id / probe_expires_at hold that lease).
--
-- generation identifies the failure epoch. It bumps on every accepted (newer)
-- failure, so a stale success that completed before the current failure cannot
-- close a fresher open circuit — closing compares (completed_at, task_id)
-- against the failure that opened the generation.
--
-- reset_at is computed by the caller from the failure reason: a parseable quota
-- limit uses the provider reset time plus grace; an opaque quota uses +1h; an
-- auth/access wall uses +4h (and never causes the selector to switch runtime).
--
-- No foreign keys or cascades (repository rule): runtime and task lifetimes are
-- managed in application code. Rows are removed when the runtime is torn down
-- and when the workspace is deleted. workspace_id is carried on the row so both
-- teardown paths filter by workspace without joining back to agent_runtime.
CREATE TABLE IF NOT EXISTS runtime_provider_circuit (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    provider TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'closed' CHECK (state IN ('closed', 'open', 'half_open')),
    generation BIGINT NOT NULL DEFAULT 0 CHECK (generation >= 0),
    reason TEXT,
    opened_at TIMESTAMPTZ,
    reset_at TIMESTAMPTZ,
    failure_completed_at TIMESTAMPTZ,
    failure_task_id UUID,
    success_completed_at TIMESTAMPTZ,
    success_task_id UUID,
    probe_task_id UUID,
    probe_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
