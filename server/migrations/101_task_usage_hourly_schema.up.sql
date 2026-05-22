-- Hourly rollup table for `task_usage`, materialised in **UTC**. Replaces
-- both per-runtime `task_usage_daily` (073, 082) and per-workspace
-- `task_usage_dashboard_daily` (084) as the single source of truth for
-- all token-usage reports. See docs/timezone-architecture-rfc.md §4.
--
-- WHY HOURLY + UTC:
--   The two existing rollups materialise on a `DATE` bucket — one in the
--   runtime's IANA tz, the other in UTC — which forces every report to
--   either accept the materialised tz or scan raw `task_usage`. Hourly
--   UTC buckets are tz-neutral: any viewer-side tz can be applied at
--   query time via `DATE(bucket_hour AT TIME ZONE @tz)` without losing
--   precision and without crossing midnight in the wrong direction.
--
-- WHY ONE TABLE INSTEAD OF TWO:
--   The two existing rollups share the same source rows and the same
--   invalidation surface (atq, task_usage, issue.project_id); maintaining
--   them separately is duplicative. The unified PK carries runtime_id,
--   agent_id, AND project_id, so:
--     * Runtime-detail views filter on runtime_id (covered by
--       idx_..._runtime_time).
--     * Workspace-dashboard views filter on workspace_id + group by
--       agent_id / project_id (covered by the three workspace indexes).
--     * The hour-of-day heatmap groups by EXTRACT(HOUR FROM ... AT TIME
--       ZONE <viewer's tz>) over the same rows — no separate aggregate.
--
-- WHY PROVIDER+MODEL IN THE PK:
--   Per-model breakdowns are a primary read dimension (cost per model,
--   trend per model). Keeping them in the PK keeps the rollup pre-grouped
--   along the same axis the UI uses.
--
-- WHY `UNIQUE NULLS NOT DISTINCT`:
--   `project_id` is nullable — tasks linked to issues without a project,
--   and the quick-create path's "no issue yet" state, both produce
--   no-project usage. PG15's `UNIQUE NULLS NOT DISTINCT` lets ON CONFLICT
--   upsert the no-project bucket the same way it handles a concrete
--   project. (Same pattern as 084.)
CREATE TABLE task_usage_hourly (
    bucket_hour         TIMESTAMPTZ NOT NULL,   -- UTC, truncated to hour boundary
    workspace_id        UUID        NOT NULL,
    runtime_id          UUID        NOT NULL,
    agent_id            UUID        NOT NULL,
    project_id          UUID,                   -- nullable; see above
    provider            TEXT        NOT NULL,
    model               TEXT        NOT NULL,
    input_tokens        BIGINT      NOT NULL DEFAULT 0,
    output_tokens       BIGINT      NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT      NOT NULL DEFAULT 0,
    cache_write_tokens  BIGINT      NOT NULL DEFAULT 0,
    task_count          BIGINT      NOT NULL DEFAULT 0,
    event_count         BIGINT      NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_usage_hourly_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
);

-- Workspace-wide trend (no other filter): /{slug}/dashboard. The leading
-- workspace_id matches every dashboard query; bucket_hour DESC avoids an
-- extra sort when the report walks "last 7/30/90 days" backwards.
CREATE INDEX idx_task_usage_hourly_workspace_time
    ON task_usage_hourly (workspace_id, bucket_hour DESC);

-- Runtime detail page — trend + hour-of-day heatmap on a single runtime.
-- The heatmap groups by `EXTRACT(HOUR FROM bucket_hour AT TIME ZONE
-- <viewer's tz>)` over this range, so we want the rows pre-clustered
-- by runtime.
CREATE INDEX idx_task_usage_hourly_runtime_time
    ON task_usage_hourly (runtime_id, bucket_hour DESC);

-- Workspace dashboard "by agent" panel.
CREATE INDEX idx_task_usage_hourly_workspace_agent_time
    ON task_usage_hourly (workspace_id, agent_id, bucket_hour DESC);

-- Workspace dashboard "by project" panel. Partial because no-project
-- buckets aggregate into a separate bucket and the panel filters them
-- out; this keeps the index small.
CREATE INDEX idx_task_usage_hourly_workspace_project_time
    ON task_usage_hourly (workspace_id, project_id, bucket_hour DESC)
    WHERE project_id IS NOT NULL;

-- Single-row state table tracking the rollup worker's watermark. Same
-- shape as 073's `task_usage_rollup_state` and 084's
-- `task_usage_dashboard_rollup_state` — a SMALLINT(1) PK is the easiest
-- way to enforce "exactly one row" without a CHECK trigger.
CREATE TABLE task_usage_hourly_rollup_state (
    id                    SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    watermark_at          TIMESTAMPTZ NOT NULL DEFAULT '1970-01-01 00:00:00+00',
    last_run_started_at   TIMESTAMPTZ,
    last_run_finished_at  TIMESTAMPTZ,
    last_run_rows         BIGINT      NOT NULL DEFAULT 0,
    last_error            TEXT
);
INSERT INTO task_usage_hourly_rollup_state (id) VALUES (1) ON CONFLICT DO NOTHING;

-- Dirty queue for invalidations the `updated_at` watermark cannot see:
--   * DELETE on `task_usage` (no row left for the watermark to catch).
--   * Cascade DELETE through `agent_task_queue` (task_usage rows gone).
--   * UPDATE of `issue.project_id` — moves the bucket to a new key,
--     OLD bucket needs to shrink, NEW bucket needs to appear.
--   * UPDATE of `agent_task_queue.runtime_id` / `agent_task_queue.issue_id`
--     — same re-attribution problem on different dimensions.
--
-- bucket_hour is computed in UTC at trigger time, so dirty keys match
-- the rollup table byte-for-byte and the window function can UNION the
-- queue into `dirty_keys` without translation.
--
-- TTL: rows in this queue MUST be pruned (see prune_task_usage_hourly_dirty
-- in the rollup-pipeline migration). Without TTL, dense workloads grow the queue
-- unboundedly — every retouched
-- bucket leaves a row behind. The window function deletes rows whose
-- enqueued_at < p_to as part of each tick, which keeps the steady state
-- bounded; the explicit prune is a belt-and-braces guarantee for rows
-- that somehow escape the window (e.g. crash mid-tick).
CREATE TABLE task_usage_hourly_dirty (
    bucket_hour   TIMESTAMPTZ NOT NULL,
    workspace_id  UUID        NOT NULL,
    runtime_id    UUID        NOT NULL,
    agent_id      UUID        NOT NULL,
    project_id    UUID,
    provider      TEXT        NOT NULL,
    model         TEXT        NOT NULL,
    enqueued_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_task_usage_hourly_dirty_key
        UNIQUE NULLS NOT DISTINCT
        (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
);

-- The window function drains rows with enqueued_at < p_to; the prune
-- helper (prune_task_usage_hourly_dirty) deletes rows
-- whose enqueued_at falls outside the retention horizon. Both scans
-- use this index.
CREATE INDEX idx_task_usage_hourly_dirty_enqueued_at
    ON task_usage_hourly_dirty (enqueued_at);
