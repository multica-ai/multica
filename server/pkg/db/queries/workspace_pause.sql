-- CEREBRO-PATCH(sqlc-workspace-pause): cerebro modification of upstream file
-- Fork-specific: workspace-level kill switch for agent tasks.
-- Kept in a dedicated file so upstream-merges don't conflict on the canonical workspace.sql / agent.sql.

-- name: IsWorkspacePaused :one
-- Returns true when the workspace has settings.tasks_paused = true.
-- COALESCE handles NULL settings and missing key both as false.
SELECT COALESCE((settings->>'tasks_paused')::boolean, false)::boolean AS paused
FROM workspace
WHERE id = $1;

-- name: SetWorkspaceTasksPaused :one
-- Atomically sets the tasks_paused flag and bumps updated_at.
UPDATE workspace
SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{tasks_paused}', to_jsonb(@paused::boolean), true),
    updated_at = now()
WHERE id = @workspace_id
RETURNING *;

-- name: CancelAllActiveTasksByWorkspace :many
-- Cancels every queued/dispatched/running task whose workspace resolves to @workspace_id.
-- Uses the same workspace-resolution logic as TaskService.ResolveTaskWorkspaceID:
--   - issue tasks → via issue.workspace_id
--   - chat tasks  → via chat_session.workspace_id
--   - autopilot   → via autopilot.workspace_id (joined through autopilot_run)
-- Returns the cancelled rows so the service layer can broadcast cancellation events.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE status IN ('queued', 'dispatched', 'running')
  AND (
    issue_id IN (SELECT i.id FROM issue i WHERE i.workspace_id = @workspace_id)
    OR chat_session_id IN (SELECT cs.id FROM chat_session cs WHERE cs.workspace_id = @workspace_id)
    OR autopilot_run_id IN (
      SELECT ar.id FROM autopilot_run ar
      JOIN autopilot ap ON ap.id = ar.autopilot_id
      WHERE ap.workspace_id = @workspace_id
    )
  )
RETURNING *;
