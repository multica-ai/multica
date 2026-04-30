-- One-shot recovery: re-enqueue agent-assigned issues whose only task
-- chain ended in 'failed' before the retry-logic feature shipped.
--
-- Idempotent: only creates a new task when no queued/dispatched/running
-- task exists for that (issue, agent) pair. Skips issues whose assigned
-- agent's runtime is currently offline (those would just fail again).
--
-- Usage:
--   psql -v workspace_slug=schieber -v cutoff='1 day' \
--        -f server/scripts/reenqueue_stuck_issues.sql
--
-- Variables:
--   :workspace_slug  workspace to recover. Required.
--   :cutoff          interval; only re-enqueue issues whose latest task
--                    failed within this window. Default '7 days'.
--                    Pass as a quoted Postgres interval string.
--
-- After running, daemons will pick up the new queued tasks on their next
-- claim cycle (~1.5s).

\if :{?workspace_slug}
\else
    \echo 'ERROR: -v workspace_slug=<slug> is required'
    \quit
\endif

\if :{?cutoff}
\else
    \set cutoff '7 days'
\endif

BEGIN;

WITH ws AS (
    SELECT id FROM workspace WHERE slug = :'workspace_slug'
), candidates AS (
    SELECT i.id          AS issue_id,
           i.assignee_id AS agent_id,
           a.runtime_id  AS runtime_id,
           i.priority    AS priority
    FROM issue i
    JOIN ws        ON ws.id = i.workspace_id
    JOIN agent a   ON a.id = i.assignee_id
    JOIN agent_runtime r ON r.id = a.runtime_id
    WHERE i.status         = 'todo'
      AND i.assignee_type  = 'agent'
      AND i.assignee_id    IS NOT NULL
      AND r.status         = 'online'
      AND a.archived_at    IS NULL
      AND NOT EXISTS (
          -- skip issues that already have a task in flight
          SELECT 1 FROM agent_task_queue active
          WHERE active.issue_id = i.id
            AND active.agent_id = i.assignee_id
            AND active.status IN ('queued', 'dispatched', 'running')
      )
      AND EXISTS (
          -- only recover issues whose latest task failed within the cutoff
          SELECT 1 FROM agent_task_queue last
          WHERE last.issue_id = i.id
            AND last.agent_id = i.assignee_id
            AND last.status = 'failed'
            AND last.completed_at > now() - (:'cutoff')::interval
      )
)
INSERT INTO agent_task_queue
    (agent_id, runtime_id, issue_id, status, priority,
     -- map enum priority to numeric, mirroring TaskService.priorityToInt
     attempt, max_attempts)
SELECT agent_id, runtime_id, issue_id, 'queued',
       CASE priority
           WHEN 'urgent' THEN 4
           WHEN 'high'   THEN 3
           WHEN 'medium' THEN 2
           WHEN 'low'    THEN 1
           ELSE               0
       END,
       1, 2
FROM candidates
RETURNING id, issue_id, agent_id;

COMMIT;
