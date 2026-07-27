-- provider_failover_handoff is the auditable ledger AND ownership record for the
-- bidirectional provider usage/rate-limit failover policy (td-836aa9). One row per failed
-- task the policy evaluated: shadow-mode records what it WOULD do; active-mode
-- rows additionally own the task chain (see the partial unique index in
-- migration 226) so a late primary completion can be discarded deterministically.
--
-- No foreign keys / cascades by repository rule: original_task_id,
-- chain_root_task_id, issue_id, chat_session_id, source_agent_id, target_agent_id
-- and fallback_task_id all reference other rows but are resolved and cleaned up
-- in application code. Deleting a task/issue does not cascade here; the ledger is
-- an append-mostly audit trail and stale references are tolerated by readers.
CREATE TABLE IF NOT EXISTS provider_failover_handoff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    -- The failed primary task this decision is about (persisted linkage).
    original_task_id UUID NOT NULL,
    -- Stable root of the task chain, used for at-most-one-per-chain + loop
    -- prevention. Equals chat_input_task_id for chat chains, the origin task for
    -- a fallback task, else the original task's id.
    chain_root_task_id UUID NOT NULL,
    issue_id UUID,
    chat_session_id UUID,
    -- The failed run's agent + runtime provider. Codex and Claude are real
    -- failover sources; other providers are recorded only as declined/shadow rows. See
    -- providerfailover.IsFailoverSource.
    source_agent_id UUID NOT NULL,
    source_provider TEXT NOT NULL,
    -- Target is derived from the source provider; kept explicit for auditability.
    target_provider TEXT NOT NULL DEFAULT 'claude',
    -- Resolved Claude target agent + the fallback task actually created (active
    -- mode only; NULL in shadow / when Claude is unavailable).
    target_agent_id UUID,
    fallback_task_id UUID,
    -- The taskfailure.Reason that triggered evaluation (a usage/rate-limit reason).
    trigger_reason TEXT NOT NULL,
    -- Lifecycle state; see server/pkg/providerfailover/state.go. Keep this CHECK
    -- in lockstep with the HandoffState constants.
    state TEXT NOT NULL CHECK (state IN (
        'HANDOFF_PENDING',
        'HANDOFF_DISPATCHED',
        'HANDOFF_COMPLETED',
        'HANDOFF_FAILED',
        'HANDOFF_SHADOW',
        'HANDOFF_DECLINED'
    )),
    -- Operational posture that produced this row.
    mode TEXT NOT NULL CHECK (mode IN ('shadow', 'active')),
    -- Whether the pure policy judged the run eligible for a real handoff,
    -- independent of mode. The point of a shadow record: "active would have
    -- handed off (or not), and why".
    would_fail_over BOOLEAN NOT NULL,
    -- Machine-readable eligibility/decline code (providerfailover.Reason*).
    decline_reason TEXT,
    -- The side-effect ledger snapshot consulted before the fallback decision.
    side_effects JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Shadow/declined rows are non-owning and must never carry a dispatched
    -- fallback task; owning rows may. Cheap structural guard against a
    -- mis-transitioned row.
    CHECK (mode = 'active' OR fallback_task_id IS NULL)
);
