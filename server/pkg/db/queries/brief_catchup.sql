-- The brief catch-up (spec 2.9, plan 9.13). Every query filters by workspace_id
-- or by an id the caller already scoped; there are no foreign keys.

-- name: ListBriefCatchupCandidates :many
-- The daily brief autopilots catch-up may fire: active and run-only, assigned to
-- the workspace's current Secretary, with an enabled schedule. One row per
-- autopilot, with its earliest enabled schedule, whose timezone sets the local day.
SELECT DISTINCT ON (ap.id)
    ap.id AS autopilot_id,
    ap.workspace_id,
    ap.assignee_id AS secretary_agent_id,
    t.id AS trigger_id,
    t.timezone AS trigger_timezone,
    w.settings AS workspace_settings
FROM autopilot ap
JOIN workspace w ON w.id = ap.workspace_id AND w.secretary_agent_id = ap.assignee_id
JOIN autopilot_trigger t ON t.autopilot_id = ap.id AND t.enabled AND t.kind = 'schedule'
WHERE ap.title = @title
  AND ap.assignee_type = 'agent'
  AND ap.execution_mode = 'run_only'
  AND ap.status = 'active'
ORDER BY ap.id, t.created_at, t.id;

-- name: CountDailyBriefsBetween :one
-- Daily briefs published in the window. The window is one local day.
SELECT count(*) FROM brief
WHERE workspace_id = @workspace_id
  AND kind = 'daily'
  AND created_at >= @window_start
  AND created_at < @window_end;

-- name: CountCatchupRunsBetween :one
-- Catch-up runs of this autopilot triggered in the window, so one local day
-- fires at most one catch-up run.
SELECT count(*) FROM autopilot_run
WHERE autopilot_id = @autopilot_id
  AND source = 'catchup'
  AND triggered_at >= @window_start
  AND triggered_at < @window_end;

-- name: CountInFlightAutopilotRuns :one
-- Runs of this autopilot that are queued or running. A skipped run is terminal
-- and does not count.
SELECT count(*) FROM autopilot_run
WHERE autopilot_id = @autopilot_id
  AND status IN ('pending', 'issue_created', 'running');

-- name: CountActiveTasksForAgent :one
-- The agent's queued, dispatched, running or deferred work: any run it is still
-- owed or doing.
SELECT count(*) FROM agent_task_queue
WHERE agent_id = @agent_id
  AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred');
