-- CEREBRO-PATCH(model-usage-event-ingestion): FIR-3337 appends one canonical
-- measurement while deriving immutable workspace/issue/agent/session lineage
-- from the daemon-owned task instead of trusting runtime-supplied attribution.
-- name: InsertModelUsageEvent :one
WITH RECURSIVE ancestors AS (
    SELECT c.id, c.parent_id
    FROM comment c
    JOIN agent_task_queue lineage_task ON lineage_task.trigger_comment_id = c.id
    WHERE lineage_task.id = sqlc.arg(task_id)
    UNION ALL
    SELECT c.id, c.parent_id
    FROM comment c
    JOIN ancestors a ON a.parent_id = c.id
),
inserted AS (
INSERT INTO model_usage_event (
    schema_version, event_id,
    task_id, workspace_id, issue_id, agent_id, runtime_id,
    session_root_comment_id, chat_session_id, autopilot_run_id, parent_task_id,
    provider_session_id, call_id, sequence, observed_at, provider, model,
    input_tokens, output_tokens, reasoning_tokens,
    cache_read_tokens, cache_write_tokens, cost_cents,
    context_tokens, context_window_tokens, compaction_kind,
    source, completeness, counter_semantics
)
SELECT
    sqlc.arg(schema_version), sqlc.arg(event_id),
    atq.id, COALESCE(i.workspace_id, cs.workspace_id, ap.workspace_id, a.workspace_id), atq.issue_id, atq.agent_id, atq.runtime_id,
    (SELECT id FROM ancestors WHERE parent_id IS NULL LIMIT 1),
    atq.chat_session_id, atq.autopilot_run_id, atq.parent_task_id,
    NULLIF(sqlc.arg(provider_session_id)::text, ''),
    NULLIF(sqlc.arg(call_id)::text, ''),
    sqlc.arg(sequence), sqlc.arg(observed_at), sqlc.arg(provider), sqlc.arg(model),
    sqlc.arg(input_tokens), sqlc.arg(output_tokens), sqlc.arg(reasoning_tokens),
    sqlc.arg(cache_read_tokens), sqlc.arg(cache_write_tokens), sqlc.arg(cost_cents),
    sqlc.arg(context_tokens), sqlc.arg(context_window_tokens), sqlc.arg(compaction_kind),
    sqlc.arg(source), sqlc.arg(completeness), sqlc.arg(counter_semantics)
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
-- CEREBRO-PATCH(model-usage-event-optional-issue-scope): FIR-3337 keeps non-issue chat/autopilot usage.
LEFT JOIN chat_session cs ON cs.id = atq.chat_session_id
LEFT JOIN autopilot_run ar ON ar.id = atq.autopilot_run_id
LEFT JOIN autopilot ap ON ap.id = ar.autopilot_id
WHERE atq.id = sqlc.arg(task_id)
  AND COALESCE(i.workspace_id, cs.workspace_id, ap.workspace_id, a.workspace_id) IS NOT NULL
ON CONFLICT DO NOTHING
RETURNING 1
)
SELECT EXISTS (SELECT 1 FROM inserted) AS inserted;

-- name: GetModelUsageEventTaskReconciliation :one
-- Shadow-only comparison during FIR-3337. Consumers continue reading the
-- legacy tables until these drifts stay at zero across real runtime traffic.
WITH cumulative_latest AS (
    SELECT DISTINCT ON (provider, model, COALESCE(provider_session_id, ''))
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents
    FROM model_usage_event
    WHERE task_id = $1
      AND counter_semantics = 'cumulative'
    ORDER BY
        provider,
        model,
        COALESCE(provider_session_id, ''),
        sequence DESC,
        observed_at DESC,
        created_at DESC
),
canonical_events AS (
    SELECT
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents
    FROM model_usage_event
    WHERE task_id = $1
      AND counter_semantics = 'delta'
    UNION ALL
    SELECT
        input_tokens,
        output_tokens,
        reasoning_tokens,
        cache_read_tokens,
        cache_write_tokens,
        cost_cents
    FROM cumulative_latest
),
ledger AS (
    SELECT
        COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(output_tokens + reasoning_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(cost_cents), 0)::bigint AS cost_cents
    FROM canonical_events
),
legacy AS (
    SELECT
        COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(cost_cents), 0)::bigint AS cost_cents
    FROM task_usage
    WHERE task_id = $1
),
ledger_context AS (
    SELECT context_tokens
    FROM model_usage_event
    WHERE task_id = $1
      AND context_tokens > 0
    ORDER BY observed_at DESC, sequence DESC, created_at DESC
    LIMIT 1
),
legacy_context AS (
    SELECT input_tokens AS context_tokens
    FROM cerebro_task_context_footprint
    WHERE task_id = $1
)
SELECT
    (SELECT COUNT(*) FROM model_usage_event mue_count WHERE mue_count.task_id = $1)::bigint AS event_count,
    (ledger.input_tokens - legacy.input_tokens)::bigint AS input_token_drift,
    (ledger.output_tokens - legacy.output_tokens)::bigint AS output_token_drift,
    (ledger.cache_read_tokens - legacy.cache_read_tokens)::bigint AS cache_read_token_drift,
    (ledger.cache_write_tokens - legacy.cache_write_tokens)::bigint AS cache_write_token_drift,
    (ledger.cost_cents - legacy.cost_cents)::bigint AS cost_cents_drift,
    (COALESCE((SELECT context_tokens FROM ledger_context), 0) -
        COALESCE((SELECT context_tokens FROM legacy_context), 0))::bigint AS context_token_drift
FROM ledger, legacy;

-- name: GetModelUsageEventSessionReconciliation :many
-- Shadow aggregate for one issue thread. The oldest thread may adopt tasks
-- created before the first comment, matching the existing session consumers.
WITH RECURSIVE thread(id) AS (
    SELECT sqlc.arg(session_root_comment_id)::uuid
    UNION ALL
    SELECT c.id FROM comment c JOIN thread ON c.parent_id = thread.id
),
scoped_events AS (
    SELECT mue.*
    FROM model_usage_event mue
    WHERE mue.issue_id = sqlc.arg(issue_id)
      AND (
        mue.session_root_comment_id = sqlc.arg(session_root_comment_id)
        OR (sqlc.arg(is_first)::bool AND mue.session_root_comment_id IS NULL)
      )
),
cumulative_latest AS (
    SELECT DISTINCT ON (task_id, provider, model, COALESCE(provider_session_id, ''))
        task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM scoped_events
    WHERE counter_semantics = 'cumulative'
    ORDER BY task_id, provider, model, COALESCE(provider_session_id, ''),
        sequence DESC, observed_at DESC, created_at DESC
),
canonical_events AS (
    SELECT task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM scoped_events
    WHERE counter_semantics = 'delta'
    UNION ALL
    SELECT task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM cumulative_latest
),
event_counts AS (
    SELECT agent_id, provider, model, COUNT(*)::bigint AS event_count
    FROM scoped_events
    GROUP BY agent_id, provider, model
),
latest_context_per_task AS (
    SELECT DISTINCT ON (task_id, provider, model)
        task_id, agent_id, provider, model, context_tokens
    FROM scoped_events
    WHERE context_tokens > 0
    ORDER BY task_id, provider, model, observed_at DESC, sequence DESC, created_at DESC
),
ledger AS (
    SELECT ce.agent_id, ce.provider, ce.model,
        ec.event_count,
        COALESCE(SUM(ce.input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(ce.output_tokens + ce.reasoning_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(ce.cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(ce.cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(ce.cost_cents), 0)::bigint AS cost_cents,
        COALESCE(MAX(lc.context_tokens), 0)::bigint AS context_tokens
    FROM canonical_events ce
    JOIN event_counts ec USING (agent_id, provider, model)
    LEFT JOIN (
        SELECT agent_id, provider, model, SUM(context_tokens)::bigint AS context_tokens
        FROM latest_context_per_task
        GROUP BY agent_id, provider, model
    ) lc USING (agent_id, provider, model)
    GROUP BY ce.agent_id, ce.provider, ce.model, ec.event_count
),
legacy AS (
    SELECT atq.agent_id, tu.provider, tu.model,
        COALESCE(SUM(tu.input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(tu.output_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(tu.cost_cents), 0)::bigint AS cost_cents,
        COALESCE(SUM(cf.input_tokens), 0)::bigint AS context_tokens
    FROM agent_task_queue atq
    JOIN task_usage tu ON tu.task_id = atq.id
    LEFT JOIN cerebro_task_context_footprint cf ON cf.task_id = atq.id AND cf.model = tu.model
    WHERE atq.issue_id = sqlc.arg(issue_id)
      AND (
        atq.trigger_comment_id IN (SELECT id FROM thread)
        OR (sqlc.arg(is_first)::bool AND atq.trigger_comment_id IS NULL)
      )
    GROUP BY atq.agent_id, tu.provider, tu.model
)
SELECT
    COALESCE(l.agent_id, legacy.agent_id) AS agent_id,
    COALESCE(l.provider, legacy.provider)::text AS provider,
    COALESCE(l.model, legacy.model)::text AS model,
    COALESCE(l.event_count, 0)::bigint AS event_count,
    COALESCE(l.input_tokens, 0)::bigint AS ledger_input_tokens,
    COALESCE(legacy.input_tokens, 0)::bigint AS legacy_input_tokens,
    (COALESCE(l.input_tokens, 0) - COALESCE(legacy.input_tokens, 0))::bigint AS input_token_drift,
    (COALESCE(l.output_tokens, 0) - COALESCE(legacy.output_tokens, 0))::bigint AS output_token_drift,
    (COALESCE(l.cache_read_tokens, 0) - COALESCE(legacy.cache_read_tokens, 0))::bigint AS cache_read_token_drift,
    (COALESCE(l.cache_write_tokens, 0) - COALESCE(legacy.cache_write_tokens, 0))::bigint AS cache_write_token_drift,
    (COALESCE(l.cost_cents, 0) - COALESCE(legacy.cost_cents, 0))::bigint AS cost_cents_drift,
    (COALESCE(l.context_tokens, 0) - COALESCE(legacy.context_tokens, 0))::bigint AS context_token_drift
FROM ledger l
FULL OUTER JOIN legacy
    ON legacy.agent_id = l.agent_id AND legacy.provider = l.provider AND legacy.model = l.model
ORDER BY COALESCE(l.provider, legacy.provider), COALESCE(l.model, legacy.model), COALESCE(l.agent_id, legacy.agent_id);

-- name: GetModelUsageEventIssueReconciliation :many
-- Shadow aggregate for all runs on one issue, split by immutable attribution.
WITH scoped_events AS (
    SELECT mue.* FROM model_usage_event mue WHERE mue.issue_id = $1::uuid
),
cumulative_latest AS (
    SELECT DISTINCT ON (task_id, provider, model, COALESCE(provider_session_id, ''))
        task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM scoped_events
    WHERE counter_semantics = 'cumulative'
    ORDER BY task_id, provider, model, COALESCE(provider_session_id, ''),
        sequence DESC, observed_at DESC, created_at DESC
),
canonical_events AS (
    SELECT task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM scoped_events WHERE counter_semantics = 'delta'
    UNION ALL
    SELECT task_id, agent_id, provider, model,
        input_tokens, output_tokens, reasoning_tokens,
        cache_read_tokens, cache_write_tokens, cost_cents
    FROM cumulative_latest
),
event_counts AS (
    SELECT agent_id, provider, model, COUNT(*)::bigint AS event_count
    FROM scoped_events GROUP BY agent_id, provider, model
),
latest_context_per_task AS (
    SELECT DISTINCT ON (task_id, provider, model)
        task_id, agent_id, provider, model, context_tokens
    FROM scoped_events
    WHERE context_tokens > 0
    ORDER BY task_id, provider, model, observed_at DESC, sequence DESC, created_at DESC
),
ledger AS (
    SELECT ce.agent_id, ce.provider, ce.model, ec.event_count,
        COALESCE(SUM(ce.input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(ce.output_tokens + ce.reasoning_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(ce.cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(ce.cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(ce.cost_cents), 0)::bigint AS cost_cents,
        COALESCE(MAX(lc.context_tokens), 0)::bigint AS context_tokens
    FROM canonical_events ce
    JOIN event_counts ec USING (agent_id, provider, model)
    LEFT JOIN (
        SELECT agent_id, provider, model, SUM(context_tokens)::bigint AS context_tokens
        FROM latest_context_per_task GROUP BY agent_id, provider, model
    ) lc USING (agent_id, provider, model)
    GROUP BY ce.agent_id, ce.provider, ce.model, ec.event_count
),
legacy AS (
    SELECT atq.agent_id, tu.provider, tu.model,
        COALESCE(SUM(tu.input_tokens), 0)::bigint AS input_tokens,
        COALESCE(SUM(tu.output_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(tu.cache_read_tokens), 0)::bigint AS cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
        COALESCE(SUM(tu.cost_cents), 0)::bigint AS cost_cents,
        COALESCE(SUM(cf.input_tokens), 0)::bigint AS context_tokens
    FROM agent_task_queue atq
    JOIN task_usage tu ON tu.task_id = atq.id
    LEFT JOIN cerebro_task_context_footprint cf ON cf.task_id = atq.id AND cf.model = tu.model
    WHERE atq.issue_id = $1
    GROUP BY atq.agent_id, tu.provider, tu.model
)
SELECT
    COALESCE(l.agent_id, legacy.agent_id) AS agent_id,
    COALESCE(l.provider, legacy.provider)::text AS provider,
    COALESCE(l.model, legacy.model)::text AS model,
    COALESCE(l.event_count, 0)::bigint AS event_count,
    COALESCE(l.input_tokens, 0)::bigint AS ledger_input_tokens,
    COALESCE(legacy.input_tokens, 0)::bigint AS legacy_input_tokens,
    (COALESCE(l.input_tokens, 0) - COALESCE(legacy.input_tokens, 0))::bigint AS input_token_drift,
    (COALESCE(l.output_tokens, 0) - COALESCE(legacy.output_tokens, 0))::bigint AS output_token_drift,
    (COALESCE(l.cache_read_tokens, 0) - COALESCE(legacy.cache_read_tokens, 0))::bigint AS cache_read_token_drift,
    (COALESCE(l.cache_write_tokens, 0) - COALESCE(legacy.cache_write_tokens, 0))::bigint AS cache_write_token_drift,
    (COALESCE(l.cost_cents, 0) - COALESCE(legacy.cost_cents, 0))::bigint AS cost_cents_drift,
    (COALESCE(l.context_tokens, 0) - COALESCE(legacy.context_tokens, 0))::bigint AS context_token_drift
FROM ledger l
FULL OUTER JOIN legacy
    ON legacy.agent_id = l.agent_id AND legacy.provider = l.provider AND legacy.model = l.model
ORDER BY COALESCE(l.provider, legacy.provider), COALESCE(l.model, legacy.model), COALESCE(l.agent_id, legacy.agent_id);
