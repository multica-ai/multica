-- Cerebro tasks page (JEH-900). Workspace-wide list of agent tasks with
-- filters for agent, status, time range, type (issue / chat), issue, and
-- project. Backs the /api/cerebro/tasks endpoint consumed by the
-- cerebro-tasks frontend package. Mirrors DashboardRecentTasks but adds
-- pagination, type, status, time-window, issue, and project filters.

-- name: ListCerebroTasks :many
SELECT atq.id::uuid AS task_id, atq.agent_id, atq.issue_id, atq.status,
       atq.dispatched_at, atq.started_at, atq.completed_at, atq.created_at,
       atq.chat_session_id, atq.title AS task_title,
       a.name AS agent_name, a.avatar_url AS agent_avatar_url,
       i.title AS issue_title, i.number AS issue_number,
       task_cost.input_tokens AS usage_input_tokens,
       task_cost.output_tokens AS usage_output_tokens,
       task_cost.cache_read_tokens AS usage_cache_read_tokens,
       task_cost.cache_write_tokens AS usage_cache_write_tokens,
       task_cost.model AS usage_model,
       trg_user.name AS triggered_by_name
FROM agent_task_queue atq
JOIN agent a ON a.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
LEFT JOIN LATERAL (
    SELECT
        COALESCE(SUM(tu.input_tokens), 0)::bigint  AS input_tokens,
        COALESCE(SUM(tu.output_tokens), 0)::bigint AS output_tokens,
        COALESCE(SUM(tu.cache_read_tokens), 0)::bigint  AS cache_read_tokens,
        COALESCE(SUM(tu.cache_write_tokens), 0)::bigint AS cache_write_tokens,
        MAX(tu.model) AS model
    FROM task_usage tu WHERE tu.task_id = atq.id
) AS task_cost ON true
LEFT JOIN comment trg_comment ON trg_comment.id = atq.trigger_comment_id
LEFT JOIN member trg_member ON trg_member.id = trg_comment.author_id
    AND trg_comment.author_type = 'member'
LEFT JOIN "user" trg_user ON trg_user.id = trg_member.user_id
WHERE a.workspace_id = $1
  AND (sqlc.narg('agent_id')::uuid IS NULL OR atq.agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('filter_issue_id')::uuid IS NULL OR atq.issue_id = sqlc.narg('filter_issue_id')::uuid)
  AND (sqlc.narg('filter_project_id')::uuid IS NULL OR i.project_id = sqlc.narg('filter_project_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR atq.status = sqlc.narg('status')::text)
  AND (
    sqlc.narg('task_type')::text IS NULL
    OR (sqlc.narg('task_type')::text = 'chat' AND atq.chat_session_id IS NOT NULL)
    OR (sqlc.narg('task_type')::text = 'issue' AND atq.chat_session_id IS NULL)
  )
  AND (sqlc.narg('since')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) <= sqlc.narg('until')::timestamptz)
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
  AND (sqlc.narg('filter_issue_id')::uuid IS NULL OR atq.issue_id = sqlc.narg('filter_issue_id')::uuid)
  AND (sqlc.narg('filter_project_id')::uuid IS NULL OR i.project_id = sqlc.narg('filter_project_id')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR atq.status = sqlc.narg('status')::text)
  AND (
    sqlc.narg('task_type')::text IS NULL
    OR (sqlc.narg('task_type')::text = 'chat' AND atq.chat_session_id IS NOT NULL)
    OR (sqlc.narg('task_type')::text = 'issue' AND atq.chat_session_id IS NULL)
  )
  AND (sqlc.narg('since')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) >= sqlc.narg('since')::timestamptz)
  AND (sqlc.narg('until')::timestamptz IS NULL OR
       COALESCE(atq.completed_at, atq.started_at, atq.dispatched_at, atq.created_at) <= sqlc.narg('until')::timestamptz)
  AND (sqlc.narg('q')::text IS NULL
       OR a.name ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR atq.title ILIKE ('%' || sqlc.narg('q')::text || '%')
       OR i.title ILIKE ('%' || sqlc.narg('q')::text || '%'));
