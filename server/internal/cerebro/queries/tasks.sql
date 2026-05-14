-- Cerebro tasks page (JEH-900). Workspace-wide list of agent tasks with
-- filters for agent, status, time range, and type (issue / chat). Backs the
-- /api/cerebro/tasks endpoint consumed by the cerebro-tasks frontend
-- package. Mirrors DashboardRecentTasks but adds pagination, type, status,
-- and time-window filters.

-- name: ListCerebroTasks :many
SELECT atq.id::uuid AS task_id, atq.agent_id, atq.issue_id, atq.status,
       atq.dispatched_at, atq.started_at, atq.completed_at, atq.created_at,
       atq.chat_session_id, atq.title AS task_title,
       a.name AS agent_name, a.avatar_url AS agent_avatar_url,
       i.title AS issue_title, i.number AS issue_number
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR atq.status = sqlc.narg('status')::text)
  AND (
    sqlc.narg('task_type')::text IS NULL
    OR (sqlc.narg('task_type')::text = 'chat' AND atq.chat_session_id IS NOT NULL)
    OR (sqlc.narg('task_type')::text = 'issue' AND atq.chat_session_id IS NULL)
  )
  AND (sqlc.narg('since')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('q')::text IS NULL
       OR a.name ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR atq.title ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR i.title ILIKE ('%' || sqlc.narg('q')::text || '%'))
ORDER BY COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) DESC,
         atq.id DESC
LIMIT $2 OFFSET $3;

-- name: CountCerebroTasks :one
SELECT COUNT(*)::int
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
WHERE a.workspace_id = $1
  AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR atq.status = sqlc.narg('status')::text)
  AND (
    sqlc.narg('task_type')::text IS NULL
    OR (sqlc.narg('task_type')::text = 'chat' AND atq.chat_session_id IS NOT NULL)
    OR (sqlc.narg('task_type')::text = 'issue' AND atq.chat_session_id IS NULL)
  )
  AND (sqlc.narg('since')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('q')::text IS NULL
       OR a.name ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR atq.title ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR i.title ILIKE ('%' || sqlc.narg('q')::text || '%'));
