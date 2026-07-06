-- Supporting btree index on comment.workspace_id for the search handler.
--
-- Context (MUL-4059): The search handler's WHERE clause contains a
-- correlated `EXISTS` subquery over `comment` that Postgres routinely
-- rewrites into a *hashed* subplan — the subquery is evaluated once and
-- the results are hashed for lookup by the outer scan. That rewrite is
-- an optimization when the subquery is cheap; it becomes a pathology
-- when the subquery scans the entire `comment` table filtered only by
-- LIKE, because for common tokens (e.g. "search", "agent") the bigm/trgm
-- GIN index matches hundreds of thousands of rows across every
-- workspace. Confirmed on prd against `multica-prod`: a search for
-- '%search%' returned 536,761 comment bigm hits, spilled work_mem into a
-- lossy bitmap (`Heap Blocks: exact=48297 lossy=164696`), rechecked 1.9M
-- rows, and the outer query took 32.3 s despite indexes being present.
--
-- The fix is two-part:
--   1. Query-level: buildSearchQuery now adds `c.workspace_id = $wsParam`
--      to every comment subquery. With the workspace_id as a compile-time
--      constant (same parameter as the outer WHERE), the planner can
--      collapse the hashed set to this workspace's comments only.
--   2. Index-level: this migration. Without a btree index on
--      comment.workspace_id, the pushed-down filter still triggers a
--      Seq Scan on `comment`; with it, the planner picks an Index Scan
--      or Bitmap AND with the bigm/trgm content index.
--
-- Verified on a local repro that mirrors the prd hot workspace
-- (5k issues in the target workspace, 100k comments in a sibling
-- workspace all containing "search"): the query plan drops from
-- 60 ms (hashed global scan) to 1.8 ms (subplan uses this index).
-- Prd extrapolation: 32.3 s → tens of milliseconds.
--
-- Plain CREATE INDEX (not CONCURRENTLY) is used because CONCURRENTLY
-- cannot be inside the DO block that guards the migration for
-- environments where the table shape has drifted; on prd the operator
-- can pre-create with CONCURRENTLY and the IF NOT EXISTS guard will
-- make this migration a no-op.

DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_comment_workspace
    ON comment (workspace_id);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping idx_comment_workspace (comment.workspace_id missing?)';
END
$$;
