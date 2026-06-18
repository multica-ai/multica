-- TECH-3738 Bid B — observed access for the agent capabilities card.
--
-- "Observed access" is what an agent ACTUALLY used in its recent runs, as
-- opposed to what it is merely permitted to use (the declared layers Bid A
-- surfaces). The only runtime-usage signal recorded today is the per-tool-call
-- log in task_message.tool: every tool an agent invokes during a queued task
-- writes one row. We aggregate those rows per tool over a recent window, joined
-- to the agent via the task that owns the message.
--
-- We deliberately do NOT synthesise observed *secret* access here: there is no
-- runtime record of which secret an agent read (only an admin audit of who
-- attached/revealed credentials). Claiming observed secret use from data we do
-- not have is exactly the false-confidence the review (TECH-3738) warned about.

-- name: ListAgentObservedToolUsage :many
SELECT tm.tool::text                    AS tool,
       COUNT(*)::bigint                 AS uses,
       MAX(tm.created_at)::timestamptz  AS last_used
FROM task_message tm
JOIN agent_task_queue atq ON atq.id = tm.task_id
WHERE atq.agent_id = sqlc.arg(agent_id)
  AND tm.tool IS NOT NULL
  AND tm.tool <> ''
  AND tm.created_at >= now() - make_interval(days => sqlc.arg(window_days)::int)
GROUP BY tm.tool
ORDER BY uses DESC, tool ASC;

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
SELECT tm.tool::text                    AS tool,
       COUNT(*)::bigint                 AS uses,
       MIN(tm.created_at)::timestamptz  AS first_used,
       MAX(tm.created_at)::timestamptz  AS last_used
FROM task_message tm
JOIN agent_task_queue atq ON atq.id = tm.task_id
WHERE atq.agent_id = sqlc.arg(agent_id)
  AND tm.tool IS NOT NULL
  AND tm.tool <> ''
  AND tm.created_at > sqlc.arg(window_start_at)::timestamptz
  AND tm.created_at <= sqlc.arg(window_end_at)::timestamptz
GROUP BY tm.tool
ORDER BY uses DESC, tool ASC;
