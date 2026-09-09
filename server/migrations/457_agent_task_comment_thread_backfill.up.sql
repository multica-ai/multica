-- One-shot backfill of comment_thread_id for legacy pre-451 pending and
-- in-flight rows (HYP-1750 / HYP-1754).
--
-- Why: migration 451 deliberately skipped the existing-row backfill
-- (76f59f5f1), so tasks created before it kept comment_thread_id = NULL.
-- Migration 452's partial unique index folds NULL into the zero-UUID bucket,
-- which left every IS NOT DISTINCT FROM thread fence in agent.sql matching
-- those legacy rows only against assignment-level (NULL-thread) work:
--   * a new trigger in any thread slipped past the pending dedup fence and
--     could produce a duplicate SEQUENTIAL run (ClaimAgentTask still
--     serializes per issue+agent, so never a concurrent one);
--   * MergeCommentIntoPendingTask would not fold new comments into the
--     legacy row;
--   * CancelPendingTasksByIssueAndAgentInThread could not cancel it;
--   * a legacy running / waiting_local_directory row missed
--     RegisterPlannedCommentForActiveTask's fence, so a same-thread comment
--     fell back to enqueueing a fresh task and ran twice in sequence.
-- This backfill returns legacy rows to the standard semantics every post-451
-- row already has. comment_thread_id is derived fence data, not a FK (451),
-- so rewriting it is safe.
--
-- Scope (deliberate):
--   * comment_thread_id IS NULL AND trigger_comment_id IS NOT NULL — the 451
--     trigger function is STRICT, so assignment-level tasks (no trigger
--     comment) legitimately keep a NULL thread scope and must never be
--     backfilled;
--   * statuses queued / dispatched / deferred+channel_issue_media_pending
--     (the 452 index scope — rows a new pending insert can collide with)
--     PLUS running / waiting_local_directory (the
--     RegisterPlannedCommentForActiveTask fence scope — closes the
--     duplicate-sequential-run gap for in-flight rows too);
--   * terminal rows (completed / cancelled / failed) are excluded: a fence
--     value on them has no effect and history is unbounded.
--
-- Behaviour change to be aware of (standard semantics, not a regression): a
-- backfilled running / waiting_local_directory legacy row narrows its
-- reconcileCommentsOnCompletion reconciliation scope from issue-level to
-- thread-level on completion — the same semantics every post-451 task already
-- runs under.
--
-- Collision guard: a same-(issue, agent, thread) pending row created in the
-- 453→457 window (the exact duplicate this fix addresses) already occupies
-- the target 452-index bucket. The NOT EXISTS guard below mirrors the 452
-- partial-index WHERE clause VERBATIM, plus id self-exclusion, so the
-- backfill can never trip the unique index. A colliding legacy row keeps its
-- NULL thread scope and is NOT cancelled — a migration must not silently drop
-- user work — and is counted in the RAISE NOTICE below so the deploy log
-- answers "how many rows backfilled, how many skipped on collision".
--
-- Concurrency: the candidate scan, the guard, and the UPDATE share one
-- statement snapshot, so a concurrent INSERT can still slip into a bucket
-- after the guard evaluated it; the 452 unique index backstops that race by
-- rejecting one of the two writers. A failure here is therefore LOUD and the
-- migration is safe to re-run — the conditional UPDATE is idempotent. Retry
-- on failure; deployment tooling must not skip it silently.
--
-- Deleted trigger comments: comment_thread_root_id() COALESCEs a missing
-- anchor back to the comment id itself, so a legacy row whose trigger comment
-- row is gone is backfilled to that (dead) id. That moves the row out of the
-- shared NULL bucket into a deterministic singleton bucket — harmless and
-- explicit, not an error. (Normally the ON DELETE SET NULL FK clears
-- trigger_comment_id first, which excludes the row from this backfill
-- entirely.)
DO $$
DECLARE
    v_backfilled int;
    v_collisions int;
BEGIN
    WITH legacy AS (
        SELECT t.id, t.issue_id, t.agent_id,
               comment_thread_root_id(t.trigger_comment_id) AS thread_root_id
        FROM agent_task_queue t
        WHERE t.comment_thread_id IS NULL
          AND t.trigger_comment_id IS NOT NULL
          AND (
                t.status IN ('queued', 'dispatched')
             OR (t.status = 'deferred' AND t.context->>'channel_issue_media_pending' = 'true')
             OR t.status IN ('running', 'waiting_local_directory')
          )
    ),
    collision AS (
        SELECT l.id
        FROM legacy l
        WHERE EXISTS (
            SELECT 1
            FROM agent_task_queue occupant
            WHERE occupant.id <> l.id
              AND occupant.issue_id = l.issue_id
              AND occupant.agent_id = l.agent_id
              AND COALESCE(occupant.comment_thread_id, '00000000-0000-0000-0000-000000000000'::uuid)
                  = l.thread_root_id
              -- Verbatim mirror of the 452 partial-index WHERE clause:
              AND (
                    occupant.status IN ('queued', 'dispatched')
                 OR (occupant.status = 'deferred' AND occupant.context->>'channel_issue_media_pending' = 'true')
              )
        )
    ),
    backfilled AS (
        UPDATE agent_task_queue t
        SET comment_thread_id = l.thread_root_id
        FROM legacy l
        WHERE t.id = l.id
          AND NOT EXISTS (SELECT 1 FROM collision c WHERE c.id = l.id)
        RETURNING t.id
    )
    SELECT (SELECT count(*) FROM backfilled), (SELECT count(*) FROM collision)
      INTO v_backfilled, v_collisions;

    RAISE NOTICE '457_agent_task_comment_thread_backfill: backfilled % legacy row(s); skipped % collision row(s) left NULL (bucket occupied by a newer pending task)',
        v_backfilled, v_collisions;
END $$;
