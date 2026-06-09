-- Cerebro agent wakeups: agent-requested future re-entry points.

-- name: CreateCerebroAgentWakeup :one
INSERT INTO cerebro_agent_wakeup (
    workspace_id, agent_id, issue_id, prompt, trigger_type,
    fire_at, watch_issue_id, watch_status, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, workspace_id, agent_id, issue_id, prompt, trigger_type,
          fire_at, watch_issue_id, watch_status, state, claimed_at,
          dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at;

-- name: ListCerebroAgentWakeups :many
SELECT id, workspace_id, agent_id, issue_id, prompt, trigger_type,
       fire_at, watch_issue_id, watch_status, state, claimed_at,
       dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at
FROM cerebro_agent_wakeup
WHERE workspace_id = $1
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('state')::text IS NULL OR state = sqlc.narg('state')::text)
  AND (sqlc.narg('issue_id')::uuid IS NULL OR issue_id = sqlc.narg('issue_id')::uuid)
ORDER BY created_at DESC
LIMIT $2;

-- name: GetCerebroAgentWakeup :one
SELECT id, workspace_id, agent_id, issue_id, prompt, trigger_type,
       fire_at, watch_issue_id, watch_status, state, claimed_at,
       dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at
FROM cerebro_agent_wakeup
WHERE id = $1;

-- name: CancelCerebroAgentWakeup :one
UPDATE cerebro_agent_wakeup
SET state = 'cancelled',
    cancelled_at = now(),
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND state = 'pending'
RETURNING id, workspace_id, agent_id, issue_id, prompt, trigger_type,
          fire_at, watch_issue_id, watch_status, state, claimed_at,
          dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at;

-- name: ClaimDueTimeWakeups :many
UPDATE cerebro_agent_wakeup
SET state = 'claimed',
    claimed_at = now(),
    updated_at = now()
WHERE id IN (
    SELECT id
    FROM cerebro_agent_wakeup
    WHERE cerebro_agent_wakeup.trigger_type = 'time'
      AND cerebro_agent_wakeup.state = 'pending'
      AND cerebro_agent_wakeup.fire_at <= now()
    ORDER BY cerebro_agent_wakeup.fire_at ASC
    LIMIT sqlc.arg('row_limit')
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workspace_id, agent_id, issue_id, prompt, trigger_type,
          fire_at, watch_issue_id, watch_status, state, claimed_at,
          dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at;

-- name: ClaimPendingIssueStatusWakeups :many
UPDATE cerebro_agent_wakeup
SET state = 'claimed',
    claimed_at = now(),
    updated_at = now()
WHERE id IN (
    SELECT id
    FROM cerebro_agent_wakeup
    WHERE cerebro_agent_wakeup.trigger_type = 'issue_status'
      AND cerebro_agent_wakeup.state = 'pending'
      AND cerebro_agent_wakeup.watch_issue_id = sqlc.arg('watch_issue_id')
      AND cerebro_agent_wakeup.watch_status = sqlc.arg('watch_status')
    ORDER BY created_at ASC
    LIMIT sqlc.arg('row_limit')
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workspace_id, agent_id, issue_id, prompt, trigger_type,
          fire_at, watch_issue_id, watch_status, state, claimed_at,
          dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at;

-- name: ClaimPendingGithubCIWakeups :many
UPDATE cerebro_agent_wakeup
SET state = 'claimed',
    claimed_at = now(),
    updated_at = now()
WHERE id IN (
    SELECT id
    FROM cerebro_agent_wakeup
    WHERE cerebro_agent_wakeup.trigger_type = 'github_ci'
      AND cerebro_agent_wakeup.state = 'pending'
      AND cerebro_agent_wakeup.watch_issue_id = ANY(sqlc.arg('issue_ids')::uuid[])
    ORDER BY created_at ASC
    LIMIT sqlc.arg('row_limit')
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workspace_id, agent_id, issue_id, prompt, trigger_type,
          fire_at, watch_issue_id, watch_status, state, claimed_at,
          dispatched_at, cancelled_at, failure, created_by_id, created_at, updated_at;

-- name: MarkWakeupDispatched :exec
UPDATE cerebro_agent_wakeup
SET state = 'dispatched',
    dispatched_at = now(),
    updated_at = now()
WHERE id = $1;

-- name: ReleaseWakeupToPending :exec
-- TECH-3176: a claimed wakeup whose trigger type is disabled for its workspace
-- is released back to pending so it resumes firing once the type is re-enabled.
UPDATE cerebro_agent_wakeup
SET state = 'pending',
    claimed_at = NULL,
    updated_at = now()
WHERE id = $1
  AND state = 'claimed';

-- name: MarkWakeupFailed :exec
UPDATE cerebro_agent_wakeup
SET state = 'failed',
    failure = $2,
    updated_at = now()
WHERE id = $1;

-- name: CancelPendingWakeupsByIssueID :exec
UPDATE cerebro_agent_wakeup
SET state = 'cancelled',
    cancelled_at = now(),
    updated_at = now()
WHERE issue_id = $1
  AND state = 'pending';

-- name: PostponeWakeup :exec
-- Resets a claimed wakeup back to pending as a time trigger, firing after the
-- given delay. Used when dispatch conditions aren't met (active task on issue
-- or agent runtime offline).
UPDATE cerebro_agent_wakeup
SET state = 'pending',
    trigger_type = 'time',
    fire_at = $2,
    updated_at = now()
WHERE id = $1;
