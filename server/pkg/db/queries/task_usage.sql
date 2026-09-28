-- name: UpsertTaskUsage :exec
-- Bumps `updated_at` on INSERT and on conflict so the hourly-rollup worker
-- detects the row as dirty and re-aggregates its bucket.
-- Without the conflict-side bump, a correction to historical token counts
-- would never propagate to the rollup.
-- cost_usd_ticks is the provider's own price for this usage (1e-10 USD), NULL
-- when it reports none. It is overwritten like the token counters so a
-- corrected report replaces the previous figure rather than accumulating.
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, sqlc.narg('cost_usd_ticks'), now())
ON CONFLICT (task_id, provider, model)
DO UPDATE SET
    input_tokens = EXCLUDED.input_tokens,
    output_tokens = EXCLUDED.output_tokens,
    cache_read_tokens = EXCLUDED.cache_read_tokens,
    cache_write_tokens = EXCLUDED.cache_write_tokens,
    cost_usd_ticks = EXCLUDED.cost_usd_ticks,
    updated_at = now();

-- name: GetTaskUsage :many
SELECT * FROM task_usage
WHERE task_id = $1
ORDER BY model;

-- name: ListIssueTaskUsage :many
-- Per-(task, provider, model) usage rows for every task on one issue — the
-- per-run half of GetIssueUsageSummary's issue-wide total.
--
-- The model dimension stays on the wire for the same reason the runtime and
-- dashboard usage rows keep it: cost is priced client-side from a per-model
-- rate table, and a row that has collapsed two models into one sum can no
-- longer be priced at all. The execution log sums the rows per task; the usage
-- panel shows them split.
--
-- Ordering is by task then model so the client can group by task_id in one
-- pass. Uses idx_agent_task_queue_issue_id (migration 035) + the task_usage
-- task_id index (migration 032).
SELECT
    tu.task_id,
    tu.provider,
    tu.model,
    tu.input_tokens,
    tu.output_tokens,
    tu.cache_read_tokens,
    tu.cache_write_tokens,
    tu.cost_usd_ticks
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = $1
ORDER BY tu.task_id, tu.model;

-- name: ListAgentTaskUsage :many
-- Per-(task, provider, model) usage rows for one agent's explicitly requested
-- task history. ListAgentTasks is already access-gated before this query runs;
-- the agent predicate preserves that authorization boundary, while task_ids
-- keeps hydration aligned with the exact response without an N+1 query.
SELECT
    tu.task_id,
    tu.provider,
    tu.model,
    tu.input_tokens,
    tu.output_tokens,
    tu.cache_read_tokens,
    tu.cache_write_tokens,
    tu.cost_usd_ticks
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.agent_id = sqlc.arg('agent_id')
  AND tu.task_id = ANY(sqlc.arg('task_ids')::uuid[])
ORDER BY tu.task_id, tu.model;

-- name: GetIssueUsageSummary :one
-- Keep the legacy usage aggregates intact, then report coverage over finite
-- terminal runs separately. A task_usage row is the durable evidence that a
-- run reported usage even when every token counter is legitimately zero.
-- Both passes use the existing issue_id / task_id indexes (migrations 035/032).
WITH usage AS (
    SELECT
        COALESCE(SUM(tu.input_tokens), 0)::bigint AS total_input_tokens,
        COALESCE(SUM(tu.output_tokens), 0)::bigint AS total_output_tokens,
        COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS total_cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS total_cache_write_tokens,
        COALESCE(SUM(tu.cost_usd_ticks), 0)::bigint AS total_cost_usd_ticks,
        COALESCE(SUM(tu.input_tokens)       FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_input_tokens,
        COALESCE(SUM(tu.output_tokens)      FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_output_tokens,
        COALESCE(SUM(tu.cache_read_tokens)  FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens) FILTER (WHERE tu.cost_usd_ticks IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
        COUNT(DISTINCT tu.task_id)::int AS task_count
    FROM task_usage tu
    JOIN agent_task_queue atq ON atq.id = tu.task_id
    WHERE atq.issue_id = $1
), terminal_runs AS (
    SELECT
        COUNT(*)::int AS terminal_task_count,
        COUNT(*) FILTER (WHERE EXISTS (
            SELECT 1 FROM task_usage tu WHERE tu.task_id = atq.id
        ))::int AS metered_task_count
    FROM agent_task_queue atq
    WHERE atq.issue_id = $1
      AND atq.status IN ('completed', 'failed', 'cancelled')
      AND atq.started_at IS NOT NULL
      AND atq.completed_at IS NOT NULL
)
SELECT
    usage.*,
    terminal_runs.terminal_task_count,
    terminal_runs.metered_task_count,
    (terminal_runs.terminal_task_count - terminal_runs.metered_task_count)::int AS unreported_task_count
FROM usage
CROSS JOIN terminal_runs;

-- name: ListDashboardUsageDaily :many
-- Daily per-(date, provider, model) token aggregates for the workspace, served
-- from the UTC-bucketed `task_usage_hourly` table and
-- sliced to calendar days under the caller-supplied @tz. Optionally
-- scoped to a single project via sqlc.narg('project_id'). Powers the
-- workspace dashboard's daily cost chart.
-- The viewer's tz is applied here at query time, so a viewer in
-- Asia/Shanghai gets their "today" cut at +08 and one in
-- America/Los_Angeles gets theirs at -08 against the same UTC rows.
--
-- @since is already the viewer's local start-of-day-(N) as a UTC
-- instant (computed by parseSinceParamInTZ). It must NOT be re-truncated
-- with DATE_TRUNC here — DATE_TRUNC operates in the session tz and would
-- snap the cutoff back to UTC midnight, dragging in an extra partial
-- local day for any non-UTC viewer.
-- provider is LOWER()-normalized so mixed-case historical rows (written
-- before the handler lowercased provider on write) merge with new rows
-- instead of forming a separate case-variant bucket.
SELECT
    DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) AS date,
    LOWER(provider) AS provider,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text), LOWER(provider), model
ORDER BY DATE(bucket_hour AT TIME ZONE sqlc.arg('tz')::text) DESC, LOWER(provider), model;

-- name: ListDashboardUsageByAgent :many
-- Per-(agent, provider, model) token aggregates from `task_usage_hourly`. No
-- date grouping in the result, so this query takes no `@tz` — the
-- @since cutoff is a raw timestamptz the Go layer has already computed
-- in the viewer's tz. Model dimension is preserved so the client can
-- compute cost from its per-model pricing table; the client folds rows
-- by agent for the "by agent" list on the dashboard.
--
-- task_count is summed across hourly buckets — one task that spans
-- multiple hours lands in multiple buckets, so this over-counts by
-- hour the same way the daily version over-counted by day. The
-- frontend prefers `ListDashboardAgentRunTime` for the user-facing
-- "tasks" column, so this stays informational only.
-- provider is LOWER()-normalized so mixed-case historical rows merge with
-- new rows (see ListDashboardUsageDaily).
SELECT
    agent_id,
    LOWER(provider) AS provider,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY agent_id, LOWER(provider), model
ORDER BY agent_id, LOWER(provider), model;

-- name: ListDashboardRunTimeDaily :many
-- Daily per-date run time + task counts for the workspace, optionally
-- scoped to a single project. Powers the workspace dashboard's "Time"
-- and "Tasks" metrics on the same toggle as Tokens / Cost. Bucketed by
-- completed_at (terminal time) sliced into calendar days under the
-- caller-supplied @tz — same Viewing-tz treatment as ListDashboardUsageDaily
-- so the Time / Tasks tabs cut their day boundary identically to the
-- Cost / Tokens tabs (a viewer east of UTC would otherwise see the four
-- tabs disagree on a "1d" window). Only terminal tasks (completed, failed,
-- or cancelled) with both started_at and completed_at populated contribute.
--
-- 'cancelled' is in the filter because a run the user stopped mid-flight
-- burned real agent time and real tokens before the stop landed
-- (CancelAgentTask accepts 'running'). Excluding it zeroed that time while
-- the cost rollup — which has no status filter at all — kept charging for
-- it, so Time/Tasks and Cost/Tokens were summing different task populations
-- on the same page. The started_at guard keeps a run cancelled while still
-- queued out: it never occupied an agent.
--
-- @since is already the viewer's local start-of-day-(N) (parseSinceParamInTZ)
-- — passed straight through, NOT re-truncated; see ListDashboardUsageDaily.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count,
    COUNT(*) FILTER (WHERE atq.status = 'cancelled')::int AS cancelled_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed', 'cancelled')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text)
ORDER BY DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) DESC;

-- name: ListDashboardUsageByModel :many
-- Per-model token aggregates from `task_usage_hourly`. Groups workspace
-- usage by model for the dashboard's Model scope. No agent dimension —
-- the model field is the key.
--
-- The cost columns are the same split every other usage rollup in this file
-- carries (see ListDashboardUsageByAgent): `cost_usd_ticks` is what the
-- provider itself charged, and the `uncosted_*` sums are the tokens it did
-- NOT price, which the client estimates from its own rate table. Without
-- them the client has no authoritative half, so a model the rate table does
-- not know prices at $0.00 and this scope stops summing to the Cost KPI
-- sitting directly above it (migration 213).
--
-- @since is the viewer's local start-of-day-(N) (same convention as
-- ListDashboardUsageByAgent); passed straight through without re-truncation.
SELECT
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens,
    SUM(task_count)::int             AS task_count
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY model
ORDER BY SUM(input_tokens + output_tokens + cache_read_tokens + cache_write_tokens) DESC;

-- name: ListDashboardRuntimeDuration :many
-- Per-runtime total task run time and task count for the workspace.
-- Mirrors ListDashboardAgentRunTime but groups on runtime_id.
--
-- The terminal-task filter is that query's filter verbatim — completed,
-- failed AND cancelled, with both timestamps populated — and it must stay
-- that way. A run the user stopped mid-flight burned real agent time, and
-- dropping it here while ListDashboardAgentRunTime kept it made the same
-- selector report different totals depending on which scope was open (see
-- the 'cancelled' note on ListDashboardAgentRunTime). metered_task_count
-- and cancelled_count ride along for the same reason they do there: the
-- client needs them to split a runtime's task count into succeeded /
-- failed / cancelled exactly as it does for an agent.
--
-- @since is the viewer's local start-of-day (passed through without
-- re-truncation). EXACT N days, not N+1: this response carries no date, so
-- the client cannot trim the surplus day (MUL-5551).
SELECT
    atq.runtime_id,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE EXISTS (
        SELECT 1 FROM task_usage tu WHERE tu.task_id = atq.id
    ))::int AS metered_task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count,
    COUNT(*) FILTER (WHERE atq.status = 'cancelled')::int AS cancelled_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed', 'cancelled')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.runtime_id
ORDER BY total_seconds DESC;

-- name: ListDashboardAgentRunTime :many
-- Per-agent total task run time and task count for the workspace, optionally
-- scoped to a single project. Counts only terminal runs (completed, failed,
-- or cancelled) with both started_at and completed_at populated — queued/
-- running tasks have no finite duration. Anchored on completed_at so the
-- window matches the token cost window (which is anchored on tu.created_at,
-- ~= completion time).
--
-- See ListDashboardRunTimeDaily for why 'cancelled' belongs in the filter.
-- metered_task_count uses task_usage row existence, not token totals, so a
-- provider-reported zero stays distinct from a run that reported nothing.
--
-- No date bucketing, so no @tz — but @since is the viewer's local
-- start-of-day for the EXACT N-day window (parseExactSinceParamInTZ), so the
-- "last N days" window lines up with the per-agent cost card and the daily
-- charts the client trims to the same span; passed straight through without
-- re-truncation.
SELECT
    atq.agent_id,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (atq.completed_at - atq.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(*)::int AS task_count,
    COUNT(*) FILTER (WHERE EXISTS (
        SELECT 1 FROM task_usage tu WHERE tu.task_id = atq.id
    ))::int AS metered_task_count,
    COUNT(*) FILTER (WHERE atq.status = 'failed')::int AS failed_count,
    COUNT(*) FILTER (WHERE atq.status = 'cancelled')::int AS cancelled_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed', 'cancelled')
  AND atq.started_at IS NOT NULL
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id
ORDER BY total_seconds DESC;

-- name: ListDashboardFailuresDaily :many
-- Daily per-(date, failure_reason) terminal-task counts for the workspace,
-- optionally scoped to a single project. Powers the workspace dashboard's
-- "Errors" trend and the errors-by-class breakdown.
--
-- Shape note: this returns EVERY terminal task, not just the failures. The
-- `failure_reason = ''` row of each date carries that date's succeeded
-- count, which is the denominator the client needs for an error rate. A
-- failed row whose failure_reason column is NULL or empty (pre-MUL-1949
-- rows, or a failure path that forgot to classify) collapses into the
-- 'unclassified' bucket so it stays countable instead of masquerading as a
-- success. Cardinality is bounded by days x (21 reasons + 2), so the whole
-- window fits in one small payload.
--
-- Unlike ListDashboardRunTimeDaily this does NOT require started_at — a task
-- that expired in the queue (failure_reason='queued_expired') never started
-- but is unambiguously a failure, and dropping it would under-report exactly
-- the outage the Errors chart exists to surface. Every failure path sets
-- completed_at, so bucketing on it covers all of them.
--
-- @since is already the viewer's local start-of-day-(N) (parseSinceParamInTZ)
-- — passed straight through, NOT re-truncated; see ListDashboardUsageDaily.
SELECT
    DATE(atq.completed_at AT TIME ZONE sqlc.arg('tz')::text) AS date,
    CASE
        WHEN atq.status = 'failed'
            THEN COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified')
        ELSE ''
    END AS failure_reason,
    COUNT(*)::int AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= sqlc.arg('since')::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY 1, 2
ORDER BY 1 DESC, 2;

-- name: ListDashboardFailuresByAgent :many
-- Per-(agent, failure_reason) terminal-task counts — the "top offenders"
-- half of the dashboard's errors breakdown. Same `failure_reason = ''`
-- succeeded-bucket convention as ListDashboardFailuresDaily, so the client
-- can rank agents by failure rate rather than raw count.
--
-- No date bucketing, so no @tz — @since is the viewer's local
-- start-of-day-(N) so the window lines up with the per-agent run-time card.
SELECT
    atq.agent_id,
    CASE
        WHEN atq.status = 'failed'
            THEN COALESCE(NULLIF(atq.failure_reason, ''), 'unclassified')
        ELSE ''
    END AS failure_reason,
    COUNT(*)::int AS task_count
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND atq.status IN ('completed', 'failed')
  AND atq.completed_at IS NOT NULL
  AND atq.completed_at >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
GROUP BY atq.agent_id, 2
ORDER BY atq.agent_id, 2;

-- name: ListDashboardModelRunTime :many
-- Per-model task run time and task count, derived by joining task_usage
-- with agent_task_queue on task_id. A task that uses multiple models
-- contributes its full duration to each model — total_seconds may exceed
-- the workspace total for workspaces where tasks call multiple models.
-- Likewise, a task that runs the same model via multiple providers
-- produces multiple `task_usage` rows (UNIQUE (task_id, provider, model));
-- we collapse to one row per (task_id, model) before joining so a
-- multi-provider task's duration is attributed once per model, not
-- duplicated per provider. COUNT(DISTINCT) avoids inflating the task
-- count for multi-model tasks.
--
-- The terminal-task filter is ListDashboardAgentRunTime's filter verbatim,
-- cancelled runs included: a run the user stopped mid-flight burned real
-- agent time, and excluding it here while the agent scope counted it made
-- the Time column change as the user moved the selector. cancelled_count
-- rides along so the client splits the count the same way it does for an
-- agent. metered_task_count is deliberately absent — every row here comes
-- from a `task_usage` row existing, so it would always equal task_count;
-- ListDashboardRuntimeDuration is the one that can report a terminal run
-- that never reported usage.
--
-- The workspace / window / project predicates are evaluated INSIDE the
-- dedup subquery, not only in the outer join. Filtering only outside made
-- the DISTINCT run over every usage row on the platform before discarding
-- them, sorting on a wide key (`Sort Key: (model, atq.id)`) and spilling to
-- a temp file at larger table sizes. Scoping the subquery means the DISTINCT
-- runs over the windowed terminal-task set instead: the wide sort and its
-- spill are gone and the query measures 3-5x faster.
--
-- What that does NOT buy is an index-driven read of task_usage. Past roughly
-- 1k rows the planner stops preferring the task_id index and hash-joins the
-- whole table against the small scoped set instead, so the read stays
-- O(platform total) however small the window is. Measured during review on
-- an inflated table (scoped set pinned at 600 rows, ANALYZE at each size,
-- EXPLAIN (ANALYZE, BUFFERS)):
--
--     rows      before                 after
--     1,000     seq  15.8ms             index  0.9ms
--     201,000   seq  93ms    spill      seq  33ms   no spill
--     701,000   seq  518ms   spill      seq  96ms   spill 2r/2w
--
-- Read the middle row as the real one: the index path is a small-table win,
-- not a scaling property. The residual scan is acceptable because this
-- query is opt-in — the client requests the Model scope only while it is
-- selected, so it is not on the default dashboard load — and a mounted tab
-- re-polls it on REFETCH_INTERVAL, not per render.
--
-- Removing the scan needs a pre-aggregated per-(model, bucket) duration
-- rollup, which does not exist: task_usage_hourly carries tokens and cost
-- but no duration, and duration only exists on the terminal
-- agent_task_queue row. That is a real project, not a rewrite of this query,
-- so do not read the scoped subquery as a step towards it.
--
-- @since is the viewer's local start-of-day-(N), EXACT N days: this
-- response carries no date, so the client cannot trim the surplus calendar
-- day (MUL-5551). Consistent with the companion ListDashboardUsageByModel.
WITH scoped_task AS (
    SELECT
        atq.id,
        atq.status,
        atq.started_at,
        atq.completed_at
    FROM agent_task_queue atq
    JOIN agent a ON a.id = atq.agent_id
    LEFT JOIN issue i ON i.id = atq.issue_id
    WHERE a.workspace_id = $1
      AND atq.status IN ('completed', 'failed', 'cancelled')
      AND atq.started_at IS NOT NULL
      AND atq.completed_at IS NOT NULL
      AND atq.completed_at >= @since::timestamptz
      AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
)
SELECT
    tu.model,
    COALESCE(
        SUM(EXTRACT(EPOCH FROM (scoped_task.completed_at - scoped_task.started_at)))::bigint,
        0
    )::bigint AS total_seconds,
    COUNT(DISTINCT scoped_task.id)::int AS task_count,
    COUNT(DISTINCT scoped_task.id) FILTER (WHERE scoped_task.status = 'failed')::int AS failed_count,
    COUNT(DISTINCT scoped_task.id) FILTER (WHERE scoped_task.status = 'cancelled')::int AS cancelled_count
FROM (
    SELECT DISTINCT tu.task_id, tu.model
    FROM task_usage tu
    JOIN scoped_task ON scoped_task.id = tu.task_id
) tu
JOIN scoped_task ON scoped_task.id = tu.task_id
GROUP BY tu.model
ORDER BY total_seconds DESC;

-- name: ListDashboardUsageByRuntime :many
-- Per-(runtime_id, model) token aggregates for the workspace, read from the
-- same `task_usage_hourly` rollup as the Agent and Model scopes. The model
-- dimension is preserved so the client can compute per-model cost and sum
-- per-runtime, mirroring how ListDashboardUsageByAgent works for the agent
-- scope.
--
-- The rollup keys on runtime_id (migration 101: runtime_id UUID NOT NULL,
-- indexed (runtime_id, bucket_hour DESC)), so this scope reads the indexed,
-- workspace-filtered, bucket-windowed table instead of joining the live
-- `task_usage` rows. That is what makes the Runtime scope's tokens and cost
-- add up to the same Cost KPI as the Agent and Model scopes beside it; the
-- earlier live-`task_usage` variant covered a different task population
-- (terminal tasks only) and so could never reconcile with them.
--
-- Because the rollup is not terminal-filtered, a queued or still-running
-- task's usage counts here — exactly as it already does in the Agent and
-- Model scopes. Time still comes from ListDashboardRuntimeDuration, which is
-- terminal-only by necessity (a run in flight has no duration yet); the
-- agent scope has the same split between its token and run-time rollups.
--
-- The cost columns are the split every other usage rollup in this file
-- carries: `cost_usd_ticks` is what the provider charged, the `uncosted_*`
-- sums are the tokens it did not price and the client estimates from its own
-- rate table. Without them a model the rate table does not know prices at
-- $0.00 here while reading correctly in the Agent scope.
--
-- @since is the viewer's local start-of-day (passed through without
-- re-truncation), EXACT N days: this response carries no date, so the client
-- cannot trim the surplus day (MUL-5551).
SELECT
    runtime_id,
    model,
    SUM(input_tokens)::bigint        AS input_tokens,
    SUM(output_tokens)::bigint       AS output_tokens,
    SUM(cache_read_tokens)::bigint   AS cache_read_tokens,
    SUM(cache_write_tokens)::bigint  AS cache_write_tokens,
    SUM(cost_usd_ticks)::bigint                                          AS cost_usd_ticks,
    SUM(COALESCE(uncosted_input_tokens, input_tokens))::bigint           AS uncosted_input_tokens,
    SUM(COALESCE(uncosted_output_tokens, output_tokens))::bigint         AS uncosted_output_tokens,
    SUM(COALESCE(uncosted_cache_read_tokens, cache_read_tokens))::bigint AS uncosted_cache_read_tokens,
    SUM(COALESCE(uncosted_cache_write_tokens, cache_write_tokens))::bigint AS uncosted_cache_write_tokens
FROM task_usage_hourly
WHERE workspace_id = $1
  AND bucket_hour >= @since::timestamptz
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
GROUP BY runtime_id, model
ORDER BY runtime_id, model;
