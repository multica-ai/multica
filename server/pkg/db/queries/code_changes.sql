-- name: SetTaskCodeChanges :exec
INSERT INTO agent_task_code_changes(task_id,runtime_id,dispatched_at,snapshot,snapshot_digest,file_count)
SELECT task.id,task.runtime_id,task.dispatched_at,sqlc.arg(snapshot),sqlc.arg(snapshot_digest),sqlc.arg(file_count)
FROM agent_task_queue task JOIN issue ON issue.id=task.issue_id JOIN workspace ON workspace.id=issue.workspace_id
WHERE task.id=sqlc.arg(task_id) AND task.chat_session_id IS NULL
  AND task.status IN ('completed','failed')
  AND task.runtime_id IS NOT DISTINCT FROM sqlc.narg(captured_runtime_id)::uuid
  AND task.dispatched_at IS NOT DISTINCT FROM sqlc.narg(captured_dispatched_at)::timestamptz
  AND workspace.settings->'code_changes_enabled'='true'::jsonb
ON CONFLICT(task_id) DO NOTHING;

-- name: ListIssueCodeChangeTasks :many
SELECT sqlc.embed(task),artifact.file_count,artifact.runtime_id AS snapshot_runtime_id,artifact.dispatched_at AS snapshot_dispatched_at
FROM agent_task_queue task JOIN agent_task_code_changes artifact ON artifact.task_id=task.id
WHERE task.issue_id=$1 AND task.chat_session_id IS NULL
ORDER BY task.created_at DESC,task.id DESC;

-- name: GetTaskCodeChangesSnapshot :one
SELECT snapshot FROM agent_task_code_changes WHERE task_id=$1;

-- name: GetTaskCodeChangesReceipt :one
SELECT snapshot_digest FROM agent_task_code_changes WHERE task_id=$1;

-- name: DeleteOrphanedTaskCodeChanges :execrows
DELETE FROM agent_task_code_changes WHERE task_id IN (
 SELECT artifact.task_id FROM agent_task_code_changes artifact
 WHERE NOT EXISTS(SELECT 1 FROM agent_task_queue task WHERE task.id=artifact.task_id) LIMIT 500
);

-- name: UpdateWorkspaceCodeChanges :one
UPDATE workspace SET settings=jsonb_set(COALESCE(settings,'{}'::jsonb),'{code_changes_enabled}',to_jsonb(sqlc.arg(enabled)::boolean),true),updated_at=now()
WHERE id=$1 RETURNING *;
