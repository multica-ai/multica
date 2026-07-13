-- Backfill source_task_id on top-level agent comments produced by
-- direct-assignment runs (trigger_comment_id IS NULL).
--
-- Migration 158 covered the "parent_id → trigger_comment_id" path,
-- recovering comments that are replies to a trigger comment. This
-- migration covers the complementary case: direct-assignment runs
-- (no trigger comment) whose completion-fallback comment is a
-- top-level comment (parent_id IS NULL) and was written with an
-- empty sourceTaskID before the U1 fix (task.go:1827).
--
-- Strategy: timestamp matching. A direct-assignment run's produced
-- comment must have been created AFTER the run completed (the run
-- finishes, then the completion path creates the comment). Among
-- all direct-assignment runs on the same (issue, agent) pair whose
-- completed_at precedes the comment's created_at, pick the most
-- recently completed run — it is the one that produced the comment.
--
-- Safety properties:
-- 1. WHERE c.source_task_id IS NULL — idempotent, safe to re-run.
-- 2. DISTINCT ON (c.id) prevents a single comment from receiving
--    multiple task_id values when multiple runs qualify.
-- 3. ORDER BY c.id, t.completed_at DESC picks the most recently
--    completed run when multiple (issue, agent) runs qualify.
-- 4. The workspace guard from migration 158 is not needed here:
--    the join is on issue_id + agent_id, both of which are
--    issue-scoped and already workspace-bounded by construction.
--    A direct-assignment run can only produce comments on the same
--    issue it was assigned to.
--
-- Disambiguation quality: of the ~139 recoverable rows (measured
-- on the production dataset at time of writing), ~99 are 1:1
-- exact matches (only one direct-assignment run exists on that
-- issue+agent pair before the comment). The remaining ~40 have
-- multiple qualifying runs; the most-recent-completed heuristic
-- is correct in the overwhelming majority of cases because the
-- completion-fallback comment is created synchronously inside the
-- completion path (task.go:1796-1832), so its created_at is
-- within seconds of the run's completed_at. An incorrect match
-- is possible only when two direct-assignment runs on the same
-- issue+agent completed within seconds of each other — a rare
-- scenario whose consequence is a wrong transcript preview, not
-- data corruption.
WITH matches AS (
    SELECT DISTINCT ON (c.id)
        c.id   AS comment_id,
        t.id   AS task_id
    FROM comment c
    JOIN agent_task_queue t
        ON  t.issue_id          = c.issue_id
        AND t.agent_id          = c.author_id
        AND t.trigger_comment_id IS NULL
        AND t.completed_at IS NOT NULL
        AND t.completed_at < c.created_at
    WHERE c.author_type    = 'agent'
      AND c.source_task_id IS NULL
      AND c.parent_id      IS NULL
      AND c.type            = 'comment'
    ORDER BY c.id, t.completed_at DESC
)
UPDATE comment c
SET    source_task_id = m.task_id
FROM   matches m
WHERE  c.id = m.comment_id;