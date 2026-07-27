-- name: RecordFailoverHandoff :one
-- Persists the outcome of a single failover evaluation. Idempotent per failed
-- task via ON CONFLICT (original_task_id) DO NOTHING, so a duplicated FailTask
-- terminal callback cannot write two rows. On conflict this returns no row
-- (pgx.ErrNoRows), which callers treat as "already recorded".
--
-- For active-mode owning rows (state HANDOFF_PENDING) the chain-owner partial
-- unique index (migration 227) additionally enforces at-most-one owner per
-- chain: a concurrent claim raises a unique_violation the caller maps to the
-- max-one-per-chain decline.
INSERT INTO provider_failover_handoff (
    workspace_id, original_task_id, chain_root_task_id, issue_id, chat_session_id,
    source_agent_id, source_provider, target_provider, target_agent_id, fallback_task_id,
    trigger_reason, state, mode, would_fail_over, decline_reason, side_effects
)
VALUES (
    @workspace_id, @original_task_id, @chain_root_task_id,
    sqlc.narg(issue_id), sqlc.narg(chat_session_id),
    @source_agent_id, @source_provider, @target_provider,
    sqlc.narg(target_agent_id), sqlc.narg(fallback_task_id),
    @trigger_reason, @state, @mode, @would_fail_over,
    sqlc.narg(decline_reason), @side_effects
)
ON CONFLICT (original_task_id) DO NOTHING
RETURNING *;

-- name: GetFailoverHandoffByOriginalTask :one
SELECT * FROM provider_failover_handoff
WHERE original_task_id = $1;

-- name: GetFailoverHandoffByFallbackTask :one
-- Used for the loop guard: does a task exist as some handoff's Claude fallback?
SELECT * FROM provider_failover_handoff
WHERE fallback_task_id = $1;

-- name: ChainHasOwningHandoff :one
-- Reports whether another handoff already OWNS the given chain, excluding the
-- row for the task being evaluated (so re-evaluating the same failed task never
-- counts itself). Owning = PENDING/DISPATCHED/COMPLETED, mirroring the partial
-- unique index (migration 227). Pre-check for the policy; the unique index is
-- the atomic backstop under concurrency.
SELECT EXISTS (
    SELECT 1 FROM provider_failover_handoff
    WHERE chain_root_task_id = @chain_root_task_id
      AND original_task_id <> @exclude_original_task_id
      AND state IN ('HANDOFF_PENDING', 'HANDOFF_DISPATCHED', 'HANDOFF_COMPLETED')
) AS owned;

-- name: ListFailoverHandoffsForIssue :many
-- Observability read API: every failover decision recorded for an issue, newest
-- first. Joins nothing; the ledger is self-describing.
SELECT * FROM provider_failover_handoff
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: SetFailoverHandoffState :one
-- CAS state transition: only moves the row when it is in the expected prior
-- state, so concurrent transitions can't leapfrog. Optionally stamps the
-- resolved target agent and fallback task id. Returns no row when the CAS misses.
UPDATE provider_failover_handoff
SET state = @new_state,
    target_agent_id = COALESCE(sqlc.narg(target_agent_id), target_agent_id),
    fallback_task_id = COALESCE(sqlc.narg(fallback_task_id), fallback_task_id),
    decline_reason = COALESCE(sqlc.narg(decline_reason), decline_reason),
    updated_at = now()
WHERE id = @id AND state = @expected_state
RETURNING *;

-- name: FinalizeFailoverHandoffByFallbackTask :one
-- Advances the owning handoff whose Claude fallback task just reached a terminal
-- outcome from HANDOFF_DISPATCHED to the given terminal state
-- (HANDOFF_COMPLETED / HANDOFF_FAILED). Keyed by fallback_task_id and guarded on
-- the DISPATCHED source state, so it is an idempotent no-op (returns no row) for
-- any task that is not a live dispatched fallback. Callers swallow pgx.ErrNoRows.
UPDATE provider_failover_handoff
SET state = @new_state, updated_at = now()
WHERE fallback_task_id = @fallback_task_id
  AND state = 'HANDOFF_DISPATCHED'
RETURNING *;

-- name: ListFailoverTargets :many
-- Resolves eligible failover target agents in a workspace for a given target
-- provider: a non-archived user-kind agent bound to an ONLINE runtime whose
-- provider is @target_provider. Bidirectional (td-836aa9): @target_provider is
-- 'claude' for a GPT->Claude handoff and 'codex' for a Claude->GPT handoff, so
-- one query serves both directions. System-kind and archived agents are excluded
-- structurally here; the service layer applies the remaining authority-sensitive
-- exclusions. Emptiness of this result is exactly the "target unavailable" signal.
SELECT a.*
FROM agent a
JOIN agent_runtime r ON r.id = a.runtime_id
WHERE a.workspace_id = @workspace_id
  AND a.archived_at IS NULL
  AND a.kind = 'user'
  AND a.id <> @exclude_agent_id
  AND r.provider = @target_provider
  AND r.status = 'online'
ORDER BY a.created_at ASC;

-- name: CreateFailoverTask :one
-- Clones a failed GPT primary into a fresh Claude attempt, re-targeted to the
-- resolved Claude agent + runtime. This is a CreateRetryTask analog with three
-- deliberate differences: it re-points agent_id/runtime_id to the target, forces
-- a fresh session (a cross-provider run can never resume the GPT transcript, so
-- session_id/work_dir are dropped), and records parent_task_id for the
-- retry/resume machinery. All attribution columns are inherited UNCHANGED from
-- the parent so the strict accountable==originator constraint holds and the
-- handoff is not a new attribution event. Autopilot linkage is intentionally
-- NOT carried: failover only ever runs for issue/chat tasks (the service filters
-- autopilot out before calling this).
--
-- attempt = p.attempt + 1 inherits the parent's max_attempts. This never strands
-- the child: the claim/dispatch path (ClaimAgentTask) gates only on
-- status='queued' and per-(issue,agent) serialization — it does NOT compare
-- attempt to max_attempts, so a child born with attempt > max_attempts is fully
-- claimable. The only consequence is that if this Claude fallback itself fails
-- with a retryable reason, retryEligible (attempt < ceiling) declines a further
-- auto-retry — the intended terminal behavior for an exhausted chain.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, chat_session_id,
    status, priority, trigger_comment_id, coalesced_comment_ids, trigger_summary, context,
    session_id, work_dir,
    attempt, max_attempts, parent_task_id, force_fresh_session, is_leader_task,
    squad_id, originator_user_id, accountable_user_id,
    originator_source, delegated_from_task_id, rule_version_id,
    trigger_evidence_kind, trigger_evidence_ref_id,
    chat_input_task_id
)
SELECT
    @target_agent_id, @target_runtime_id, p.issue_id, p.chat_session_id,
    'queued', p.priority, p.trigger_comment_id, p.coalesced_comment_ids, p.trigger_summary, p.context,
    NULL, NULL,
    p.attempt + 1, p.max_attempts, p.id, TRUE, p.is_leader_task,
    p.squad_id, p.originator_user_id, p.accountable_user_id,
    p.originator_source, p.delegated_from_task_id, p.rule_version_id,
    p.trigger_evidence_kind, p.trigger_evidence_ref_id,
    p.chat_input_task_id
FROM agent_task_queue p
WHERE p.id = @original_task_id
RETURNING *;

-- name: ClaimControlPlaneEffect :one
-- At-most-once claim for an ORCHESTRATOR control-plane effect (td-836aa9). The
-- first caller to claim a given effect_key inserts and gets the row back
-- (proceed with the effect); a second caller — e.g. a handed-off fallback that
-- re-planned the same orchestration — conflicts on the UNIQUE effect_key and
-- gets no row (pgx.ErrNoRows), which the service treats as "already done, skip".
-- This is what prevents a mid-orchestration handoff from double-spawning
-- children or double-promoting stages.
INSERT INTO control_plane_effect_ledger (
    workspace_id, chain_root_task_id, effect_type, effect_key, target_ref
)
VALUES (@workspace_id, @chain_root_task_id, @effect_type, @effect_key, @target_ref)
ON CONFLICT (effect_key) DO NOTHING
RETURNING *;

-- name: GetControlPlaneEffect :one
-- Reports whether a control-plane effect has already been claimed (by key).
-- Used to answer "are this chain's control-plane effects idempotency-guarded"
-- and for the observability read path.
SELECT * FROM control_plane_effect_ledger
WHERE effect_key = $1;
