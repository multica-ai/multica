-- TECH-3738 Bid B — observed access for the agent capabilities card.
--
-- "Observed access" is what an agent used or attempted in its recent runs, as
-- opposed to what it is merely permitted to use. Ordinary calls come from
-- task_message.tool; calls rejected by the immutable Task Mandate come from the
-- access Decision Ledger. Combining both prevents a real call-time rejection
-- from looking like healthy declared access.
--
-- We deliberately do NOT synthesise observed *secret* access here: there is no
-- runtime record of which secret an agent read (only an admin audit of who
-- attached/revealed credentials). Claiming observed secret use from data we do
-- not have is exactly the false-confidence the review (TECH-3738) warned about.

-- name: ListAgentObservedToolUsage :many
WITH tool_usage AS (
    SELECT tm.tool::text                   AS tool,
           COUNT(*)::bigint                AS uses,
           MAX(tm.created_at)::timestamptz AS last_used
    FROM task_message tm
    JOIN agent_task_queue atq ON atq.id = tm.task_id
    WHERE atq.agent_id = sqlc.arg(agent_id)
      AND tm.tool IS NOT NULL
      AND tm.tool <> ''
      AND tm.created_at >= now() - make_interval(days => sqlc.arg(window_days)::int)
    GROUP BY tm.tool
),
mandate_denials AS (
    SELECT observed_tool_name::text        AS tool,
           COUNT(*)::bigint                AS mandate_denials,
           MAX(created_at)::timestamptz    AS last_denied
    FROM cerebro_access_decision_ledger
    WHERE agent_id = sqlc.arg(agent_id)
      AND reason LIKE 'task mandate %'
      AND observed_tool_name <> ''
      AND created_at >= now() - make_interval(days => sqlc.arg(window_days)::int)
    GROUP BY observed_tool_name
)
SELECT COALESCE(u.tool, m.tool)::text AS tool,
       COALESCE(u.uses, 0)::bigint AS uses,
       CASE
           WHEN u.last_used IS NULL THEN m.last_denied
           WHEN m.last_denied IS NULL THEN u.last_used
           ELSE GREATEST(u.last_used, m.last_denied)
       END::timestamptz AS last_used,
       COALESCE(m.mandate_denials, 0)::bigint AS mandate_denials
FROM tool_usage u
FULL OUTER JOIN mandate_denials m ON m.tool = u.tool
ORDER BY 2 DESC, 1 ASC;

-- name: CountAgentTasksInWindow :one
-- How many of the agent's tasks recorded any message in the window. Lets the
-- card tell "this agent ran nothing recently" (not_configured) apart from "it
-- ran but invoked no logged tools" — an empty observed list must never silently
-- read as "covered".
SELECT COUNT(DISTINCT atq.id)::bigint AS task_count
FROM agent_task_queue atq
JOIN task_message tm ON tm.task_id = atq.id
WHERE atq.agent_id = sqlc.arg(agent_id)
  AND tm.created_at >= now() - make_interval(days => sqlc.arg(window_days)::int);

-- name: ListAgentObservedToolUsageBetween :many
-- Delta form for the capability scan history. Unlike ListAgentObservedToolUsage,
-- this reads only the interval since the previous persisted scan.
WITH tool_usage AS (
    SELECT tm.tool::text                   AS tool,
           COUNT(*)::bigint                AS uses,
           MIN(tm.created_at)::timestamptz AS first_used,
           MAX(tm.created_at)::timestamptz AS last_used
    FROM task_message tm
    JOIN agent_task_queue atq ON atq.id = tm.task_id
    WHERE atq.agent_id = sqlc.arg(agent_id)
      AND tm.tool IS NOT NULL
      AND tm.tool <> ''
      AND tm.created_at > sqlc.arg(window_start_at)::timestamptz
      AND tm.created_at <= sqlc.arg(window_end_at)::timestamptz
    GROUP BY tm.tool
),
mandate_denials AS (
    SELECT observed_tool_name::text        AS tool,
           COUNT(*)::bigint                AS mandate_denials,
           MIN(created_at)::timestamptz    AS first_denied,
           MAX(created_at)::timestamptz    AS last_denied
    FROM cerebro_access_decision_ledger
    WHERE agent_id = sqlc.arg(agent_id)
      AND reason LIKE 'task mandate %'
      AND observed_tool_name <> ''
      AND created_at > sqlc.arg(window_start_at)::timestamptz
      AND created_at <= sqlc.arg(window_end_at)::timestamptz
    GROUP BY observed_tool_name
)
SELECT COALESCE(u.tool, m.tool)::text AS tool,
       COALESCE(u.uses, 0)::bigint AS uses,
       CASE
           WHEN u.first_used IS NULL THEN m.first_denied
           WHEN m.first_denied IS NULL THEN u.first_used
           ELSE LEAST(u.first_used, m.first_denied)
       END::timestamptz AS first_used,
       CASE
           WHEN u.last_used IS NULL THEN m.last_denied
           WHEN m.last_denied IS NULL THEN u.last_used
           ELSE GREATEST(u.last_used, m.last_denied)
       END::timestamptz AS last_used,
       COALESCE(m.mandate_denials, 0)::bigint AS mandate_denials
FROM tool_usage u
FULL OUTER JOIN mandate_denials m ON m.tool = u.tool
ORDER BY 2 DESC, 1 ASC;
