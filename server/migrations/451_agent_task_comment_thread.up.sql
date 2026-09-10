-- Persist the queue's thread scope so uniqueness remains atomic for every
-- writer, including retries and older clients. This is derived data, not a FK.
ALTER TABLE agent_task_queue ADD COLUMN comment_thread_id uuid;

CREATE FUNCTION comment_thread_root_id(comment_id uuid) RETURNS uuid
LANGUAGE sql STABLE STRICT AS $$
    WITH RECURSIVE ancestors AS (
        SELECT c.id, c.parent_id, c.issue_id, ARRAY[c.id] AS path
        FROM comment c WHERE c.id = comment_id
        UNION ALL
        SELECT p.id, p.parent_id, p.issue_id, a.path || p.id
        FROM ancestors a JOIN comment p ON p.id = a.parent_id AND p.issue_id = a.issue_id
        WHERE NOT p.id = ANY(a.path)
    )
    SELECT COALESCE(
        (SELECT id FROM ancestors ORDER BY cardinality(path) DESC LIMIT 1),
        comment_id
    )
$$;

CREATE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.comment_thread_id := comment_thread_root_id(NEW.trigger_comment_id);
    RETURN NEW;
END
$$;

CREATE TRIGGER agent_task_comment_thread
BEFORE INSERT OR UPDATE OF trigger_comment_id ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION set_agent_task_comment_thread();

-- Existing rows intentionally retain a NULL thread scope. Pre-migration tasks
-- drain under the issue/agent claim fence without rewriting historical data.
--
-- Transitional window (accepted, documented 2026-09; do not "fix" without
-- reading this): until every pre-migration pending row drains, that legacy
-- NULL-thread set (queued / dispatched / media-pending deferred) is INVISIBLE
-- to the thread-scoped `comment_thread_id IS NOT DISTINCT FROM
-- comment_thread_root_id(...)` fences in server/pkg/db/queries (as of this
-- writing: agent.sql cancel :643, dedup :1757/:1791, merge :1864/:1947,
-- RegisterPlannedCommentForActiveTask :1917, active-row :2162, retry lineage
-- :2246, deferred-promotion occupant checks :2290/:2364, comment.sql :385;
-- line numbers drift — grep for the predicate). Three consequences while a
-- legacy row is still pending:
--   1. A new trigger from ANY thread passes dedup that the backfilled row
--      would have blocked. Worst case is a DUPLICATE SEQUENTIAL run, not
--      concurrency — ClaimAgentTask still serializes per (issue, agent).
--   2. MergeCommentIntoPendingTask does not fold new comments into the NULL
--      row, and cancel-in-thread cannot match it.
--   3. A legacy RUNNING row misses the RegisterPlannedCommentForActiveTask
--      fence, so planned comments can attach without the active-run guard.
--
-- Why this is acceptable: the affected set is self-draining and cannot
-- reproduce. Pre-451 issue-level uniqueness bounds it to AT MOST ONE legacy
-- pending row per (issue, agent); it shrinks monotonically as live tasks
-- complete (deferred media-pending rows drain once their media arrives), and
-- the trigger above guarantees every NEW row carries a real thread scope.
-- There is no data corruption, no privilege escalation, and no persistently
-- wrong state — only a narrowing transitional window.
--
-- Future fix path, IF evidence ever shows the residual set is material: do
-- NOT backfill through the startup migration path — upstream rejected that
-- coupling (startup migrations must not rewrite data; the 451 backfill was
-- removed for exactly this, and the 457 retry PR #8229 was closed unmerged
-- for lacking production statistics plus batching/rate-limit/timeout
-- guards). Instead: (a) run a READ-ONLY candidate count grouped by
-- (issue, agent) plus EXPLAIN first — the 452 partial index keys on a
-- COALESCE expression in its third column, so a NULL-bucket probe can only
-- index-scan + filter, and any production execution needs a low
-- statement_timeout in an off-peak window; then (b) apply a bounded
-- operational backfill OUTSIDE the migration framework (batched slices,
-- row-lock contention checks, idempotent re-runnable).
