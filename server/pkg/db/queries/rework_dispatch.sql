-- name: LockReworkDispatchWorkspace :one
SELECT id FROM workspace WHERE id = @workspace_id FOR KEY SHARE;

-- name: LockReworkDispatchAgent :one
SELECT id FROM agent WHERE id = @agent_id AND workspace_id = @workspace_id FOR SHARE;

-- name: GetIssueReworkDispatchReceipt :one
SELECT r.*
FROM issue_rework_dispatch_receipt r
WHERE r.workspace_id = @workspace_id
  AND r.issue_id = @issue_id
  AND r.operation_key = @operation_key;

-- name: CreateIssueReworkDispatchReceipt :one
INSERT INTO issue_rework_dispatch_receipt (
    workspace_id, issue_id, operation_key, agent_id, payload_sha256,
    instruction, task_id, source_task_id, actor_user_id
) VALUES (
    @workspace_id, @issue_id, @operation_key, @agent_id, @payload_sha256,
    @instruction, @task_id, @source_task_id, sqlc.narg(actor_user_id)
)
RETURNING *;

-- name: FindActiveIssueTaskForAgent :one
SELECT * FROM agent_task_queue
WHERE issue_id = @issue_id
  AND agent_id = @agent_id
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
ORDER BY created_at, id
LIMIT 1;
