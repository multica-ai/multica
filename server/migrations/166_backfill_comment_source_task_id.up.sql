-- Backfill comment.source_task_id for comments that were produced by an agent
-- task but stamped with an empty value at write time.
--
-- The relationship between comment and agent_task_queue is two one-way links:
--   comment.parent_id  -> comment(id) whose (id) is agent_task_queue.trigger_comment_id
--   agent_task_queue(id) -> comment.source_task_id
--
-- Before this migration, source_task_id was only stamped by the CLI path
-- (handler/comment.go X-Task-ID header) and the per-failure system comment
-- path (task.go:1983). The synthesized-success fallback at task.go:1827
-- stamped pgtype.UUID{}, leaving the most common "agent finished, here's the
-- output" comment without any back-pointer to its producing run. U1 stamps
-- task.ID going forward; this migration recovers the back-pointer for
-- historical rows that have a parent_id resolving to a task.
--
-- Safety properties:
-- 1. JOIN through issue to enforce same-workspace scoping. agent_task_queue
--    has no workspace_id column, so workspace must be derived via the issue.
--    Without this JOIN, a data-integrity violation (cross-workspace
--    parent_id / trigger_comment_id) would silently stamp a wrong-workspace
--    task onto a comment.
-- 2. DISTINCT ON (c.id) ORDER BY c.id, t.id handles the 1:N case where
--    multiple tasks share a trigger_comment_id (e.g. retried/rerun tasks).
--    We pick the earliest task by id (creation order) — stable and
--    deterministic; the resulting run-comment link is conservative.
-- 3. WHERE c.source_task_id IS NULL ensures this migration is a no-op on
--    rows the app has already populated, so it is safe to re-run.
-- 4. We only backfill for author_type='agent' AND type IN
--    ('comment','system') because the app never creates other shapes of
--    agent-authored comments, and restricting the join key list keeps the
--    query plan cheap.
--
-- Skip categories (no comment is updated when):
--   - comment.source_task_id is already non-NULL (idempotent)
--   - comment.parent_id IS NULL (system/top-level comments with no run link)
--   - comment.parent_id points to a trigger_comment no longer resolvable
--     (the triggering comment or the agent_task_queue row was GC'd)
--   - the resolved agent_task_queue lives in a different workspace than the
--     comment (workspace guard — preserved by the JOIN issue predicate)
--   - the agent_task_queue.trigger_comment_id is itself NULL (rows created
--     before migration 028 added the column; SQL NULL = NULL excludes them)
--
-- To re-run statistics after execution, see the accompanying notes file.
UPDATE comment c
SET source_task_id = bt.task_id
FROM (
    SELECT DISTINCT ON (c2.id)
        c2.id AS comment_id,
        t2.id AS task_id
    FROM comment c2
    JOIN agent_task_queue t2
        ON c2.parent_id = t2.trigger_comment_id
    JOIN issue i
        ON i.id = t2.issue_id
       AND i.workspace_id = c2.workspace_id
    WHERE c2.source_task_id IS NULL
      AND c2.parent_id IS NOT NULL
      AND c2.author_type = 'agent'
      AND c2.type IN ('comment', 'system')
    ORDER BY c2.id, t2.id
) bt
WHERE c.id = bt.comment_id;