-- name: ControllerOwnsIssue :one
SELECT EXISTS (SELECT 1 FROM issue_controller WHERE issue_id=$1)::boolean;

-- name: LockControllerAdmission :one
SELECT c.authority_epoch,c.is_stopped,c.scope_revision,c.config,r.manifest
FROM controlled_run r JOIN issue_controller c ON c.workspace_id=r.workspace_id AND c.issue_id=r.issue_id
WHERE r.run_id=$1 FOR UPDATE OF c;

-- name: LockControllerBudget :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(budget_key)::text, 0));

-- name: CountControllerBudget :one
SELECT count(*) FROM controlled_run r JOIN agent_task_queue t ON t.id=r.run_id
WHERE r.workspace_id=sqlc.arg(workspace_id) AND r.manifest->>'budget_key'=sqlc.arg(budget_key)::text
AND t.status='running' AND t.id<>sqlc.arg(task_id);
-- name: ListControllerRuns :many
SELECT * FROM agent_task_queue WHERE issue_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1001;
