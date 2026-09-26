-- name: ListPluginActionIssues :many
-- Stable number-descending keyset pagination gives a scheduler a deterministic
-- candidate page even when positions or timestamps change concurrently.
-- status filtering takes pre-expanded concrete keys (issuestatus.ExpandCategories)
-- so built-in statuses match in workspaces whose catalog seed has not landed,
-- and the (workspace_id, status) index stays usable (MUL-6243).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at,
       i.updated_at, i.last_activity_at, i.number, i.project_id, i.metadata,
       i.stage, i.properties, i.revision, s.category AS status_category,
       count(t.id) FILTER (
         WHERE t.status IN ('queued', 'dispatched', 'running',
                            'waiting_local_directory', 'deferred')
       )::int AS active_task_count
FROM issue i
LEFT JOIN issue_status s
  ON s.workspace_id = i.workspace_id AND s.key = i.status
LEFT JOIN agent_task_queue t ON t.issue_id = i.id
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('issue_scope')::uuid IS NULL OR i.id = sqlc.narg('issue_scope')::uuid)
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status')::text)
  AND (sqlc.narg('status_keys')::text[] IS NULL OR i.status = ANY(sqlc.narg('status_keys')::text[]))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id')::uuid)
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id')::uuid)
  AND (
    sqlc.narg('has_active_tasks')::boolean IS NULL
    OR (
      EXISTS (
        SELECT 1 FROM agent_task_queue active
        WHERE active.issue_id = i.id
          AND active.status IN ('queued', 'dispatched', 'running',
                                'waiting_local_directory', 'deferred')
      ) = sqlc.narg('has_active_tasks')::boolean
    )
  )
  AND (sqlc.narg('cursor_number')::int IS NULL OR i.number < sqlc.narg('cursor_number')::int)
GROUP BY i.id, s.category
ORDER BY i.number DESC
LIMIT sqlc.arg('limit')::int;
