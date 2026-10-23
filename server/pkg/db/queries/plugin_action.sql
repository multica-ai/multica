-- Workspace agents with live queue counters, one row per agent.
--
-- Visibility mirrors ListAgents: user-kind agents that are not archived. The
-- counters split the capacity-bearing statuses of CountRunningTasks so a panel
-- can tell in-flight work from waiters blocked on a local-directory mutex:
-- running_task_count covers dispatched + running, waiting_task_count counts
-- waiting_local_directory. Archived or system agents are excluded entirely.
-- name: ListPluginActionWorkspaceAgents :many
SELECT a.id,
       a.name,
       a.model,
       a.max_concurrent_tasks,
       COUNT(atq.id) FILTER (WHERE atq.status = 'queued')::int AS queued_task_count,
       COUNT(atq.id) FILTER (WHERE atq.status IN ('dispatched', 'running'))::int AS running_task_count,
       COUNT(atq.id) FILTER (WHERE atq.status = 'waiting_local_directory')::int AS waiting_task_count
FROM agent a
LEFT JOIN agent_task_queue atq ON atq.agent_id = a.id
  AND atq.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory')
WHERE a.workspace_id = $1 AND a.archived_at IS NULL AND a.kind = 'user'
GROUP BY a.id
ORDER BY a.created_at ASC;

-- Queries backing the read-only Plugin Action endpoints for queue and agent
-- health (GET /v1/tasks, GET /v1/agents). Both surface only what a dispatch
-- health panel needs: where work stands, not what it produced.
-- The newest $2 tasks across the caller's workspace. Issue columns are
-- optional: chat and quick-create tasks never had an issue (migration 033).
--
-- agent_task_queue has no workspace_id column, so scope through the owning
-- agent — the same tenant guard GetAgentTaskInWorkspace applies.
--
-- $3 narrows a callback grant that was issued about one issue to that issue's
-- tasks; session callers always pass NULL.
-- name: ListPluginActionWorkspaceTasks :many

SELECT atq.id,
       ag.id   AS agent_id,
       ag.name AS agent_name,
       atq.status,
       atq.wait_reason,
       atq.issue_id,
       w.issue_prefix,
       i.number AS issue_number,
       i.title  AS issue_title,
       atq.created_at
FROM agent_task_queue atq
JOIN agent ag ON ag.id = atq.agent_id
LEFT JOIN issue i ON i.id = atq.issue_id
LEFT JOIN workspace w ON w.id = i.workspace_id
WHERE ag.workspace_id = $1
  AND (@issue_scope::uuid IS NULL OR atq.issue_id = @issue_scope)
ORDER BY atq.created_at DESC, atq.id DESC
LIMIT $2;