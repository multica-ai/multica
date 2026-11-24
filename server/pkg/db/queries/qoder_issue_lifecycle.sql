-- name: LockQoderTaskIssue :one
-- The task row is locked by the lifecycle transition before locking its issue.
SELECT i.* FROM issue i
JOIN agent_task_queue t ON t.issue_id = i.id
JOIN agent_runtime r ON r.id = t.runtime_id AND r.workspace_id = i.workspace_id
WHERE t.id = sqlc.arg('task_id') AND r.provider = 'qoder_cloud'
  AND i.assignee_type = 'agent' AND i.assignee_id = t.agent_id
  AND NOT EXISTS (
    SELECT 1 FROM agent_task_queue newer
    WHERE newer.issue_id = i.id AND (newer.created_at, newer.id) > (t.created_at, t.id)
  )
FOR UPDATE OF i;

-- name: SetQoderIssueRevision :exec
UPDATE agent_task_queue SET context = jsonb_set(
    COALESCE(context, '{}'::jsonb), '{qoder_issue_revision}', to_jsonb(sqlc.arg('revision')::bigint)
) WHERE id = sqlc.arg('task_id');
