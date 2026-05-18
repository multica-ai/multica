-- Backfill the `position` column so existing issues get sparse, deterministic
-- values inside each (workspace_id, status) bucket. Until now all rows wrote
-- `position = 0`, which collapsed the fractional indexing scheme (the (prev+next)/2
-- midpoint between two zeros is still zero, so mid-list drag-drop was a no-op).
--
-- Two contracts this migration MUST satisfy (MUL-2314 reviewer note #4):
--
--   1. Order-preserving. Re-rank within each bucket by
--      `position ASC, created_at DESC, id DESC` — the same tail the runtime
--      query uses for "newest first under legacy position-asc clients".
--      Buckets where users already drag-edited positions (so they have varying
--      values, not all-zero) keep their ordering; we just respace them onto
--      sparse floats.
--
--   2. Idempotent. Safe to re-run on a partially-migrated database. A bucket
--      whose positions are already sparse (MIN(position) < MAX(position))
--      is skipped entirely. After one successful pass every dense bucket
--      becomes sparse, so subsequent runs are no-ops. This matters because
--      deploy retries and side-by-side migration probes (`migrate-up --dry-run`
--      then real) used to overwrite the first run's output, including any
--      manual drag-edits that landed between probe and real.
--
-- Batching: there is no LIMIT here on purpose. The migration runs once during
-- deploy windowing, not on a hot loop. For workspaces that have grown large
-- enough to warrant batched backfill, the project runbook documents running this
-- SQL in chunks of 1000-10000 rows during a low-traffic window with a snapshot
-- of `position` taken beforehand so a one-shot rollback is possible.
WITH dense_buckets AS (
    -- A bucket is "dense" (needs backfill) iff every row shares the same
    -- position value — typically all zeros from the pre-MUL-2314 default.
    -- Pre-existing sparse buckets (any drag-edited workspace, or a re-run
    -- on the output of an earlier pass) are skipped.
    SELECT workspace_id, status
    FROM issue
    GROUP BY workspace_id, status
    HAVING MIN(position) = MAX(position)
),
ranked AS (
    SELECT i.id,
           ROW_NUMBER() OVER (
               PARTITION BY i.workspace_id, i.status
               ORDER BY i.position ASC, i.created_at DESC, i.id DESC
           )::float8 AS new_pos
    FROM issue i
    JOIN dense_buckets d ON d.workspace_id = i.workspace_id AND d.status = i.status
)
UPDATE issue
SET position = ranked.new_pos
FROM ranked
WHERE issue.id = ranked.id;

-- Composite index used by GetMinIssuePosition / GetIssueNeighborGap on the
-- create path and by the bucket-scoped rebalance worker. `IF NOT EXISTS` so
-- this is safe to re-run if an earlier deploy left the index behind.
CREATE INDEX IF NOT EXISTS idx_issue_workspace_status_position
    ON issue (workspace_id, status, position);
