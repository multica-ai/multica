-- The company's operating state and quiet hours live on the workspace row
-- (spec 4.5). Reads take no lock; transitions lock the row first.

-- name: GetWorkspaceCompanyState :one
SELECT company_state, company_state_updated_at, quiet_enabled, quiet_start, quiet_end, quiet_timezone
FROM workspace
WHERE id = sqlc.arg('workspace_id')::uuid;

-- name: LockWorkspaceCompanyState :one
SELECT company_state, company_state_updated_at, quiet_enabled, quiet_start, quiet_end, quiet_timezone
FROM workspace
WHERE id = sqlc.arg('workspace_id')::uuid
FOR UPDATE;

-- name: SetWorkspaceCompanyState :one
UPDATE workspace
SET company_state = sqlc.arg('company_state')::text,
    company_state_updated_at = now()
WHERE id = sqlc.arg('workspace_id')::uuid
RETURNING company_state, company_state_updated_at, quiet_enabled, quiet_start, quiet_end, quiet_timezone;

-- name: SetWorkspaceQuietHours :one
UPDATE workspace
SET quiet_enabled = sqlc.arg('quiet_enabled')::boolean,
    quiet_start = sqlc.arg('quiet_start')::time,
    quiet_end = sqlc.arg('quiet_end')::time,
    quiet_timezone = sqlc.arg('quiet_timezone')::text,
    company_state_updated_at = now()
WHERE id = sqlc.arg('workspace_id')::uuid
RETURNING company_state, company_state_updated_at, quiet_enabled, quiet_start, quiet_end, quiet_timezone;

-- name: ListRunningTasksInWorkspace :many
-- Claimed and running tasks of the workspace's agents. Pause and stop cancels
-- the routine and delegated ones among them. Queued tasks stay queued and the
-- claim gate holds them.
SELECT t.* FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
WHERE a.workspace_id = sqlc.arg('workspace_id')::uuid
  AND t.status IN ('dispatched', 'running')
ORDER BY t.id;

-- name: GetAutopilotRunClassInputs :one
-- What run class resolution reads from the autopilot run that enqueued a task.
SELECT r.source, ap.title, ap.assignee_type, ap.assignee_id, w.secretary_agent_id
FROM autopilot_run r
JOIN autopilot ap ON ap.id = r.autopilot_id
JOIN workspace w ON w.id = ap.workspace_id
WHERE r.id = sqlc.arg('autopilot_run_id')::uuid;
