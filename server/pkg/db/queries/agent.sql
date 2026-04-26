-- name: ListAgents :many
SELECT * FROM agent
WHERE workspace_id = $1 AND archived_at IS NULL
ORDER BY created_at ASC;

-- name: ListAllAgents :many
SELECT * FROM agent
WHERE workspace_id = $1
ORDER BY created_at ASC;

-- name: GetAgent :one
SELECT * FROM agent
WHERE id = $1;

-- name: GetAgentInWorkspace :one
SELECT * FROM agent
WHERE id = $1 AND workspace_id = $2;

-- name: CreateAgent :one
INSERT INTO agent (
    workspace_id, name, description, avatar_url, runtime_mode,
    runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id,
    instructions, custom_env, custom_args, mcp_config, model
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: UpdateAgent :one
UPDATE agent SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    runtime_config = COALESCE(sqlc.narg('runtime_config'), runtime_config),
    runtime_mode = COALESCE(sqlc.narg('runtime_mode'), runtime_mode),
    runtime_id = COALESCE(sqlc.narg('runtime_id'), runtime_id),
    visibility = COALESCE(sqlc.narg('visibility'), visibility),
    status = COALESCE(sqlc.narg('status'), status),
    max_concurrent_tasks = COALESCE(sqlc.narg('max_concurrent_tasks'), max_concurrent_tasks),
    instructions = COALESCE(sqlc.narg('instructions'), instructions),
    custom_env = COALESCE(sqlc.narg('custom_env'), custom_env),
    custom_args = COALESCE(sqlc.narg('custom_args'), custom_args),
    mcp_config = COALESCE(sqlc.narg('mcp_config'), mcp_config),
    model = COALESCE(sqlc.narg('model'), model),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearAgentMcpConfig :one
UPDATE agent SET mcp_config = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ArchiveAgent :one
UPDATE agent SET archived_at = now(), archived_by = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: RestoreAgent :one
UPDATE agent SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAgentTasks :many
SELECT * FROM agent_task_queue
WHERE agent_id = $1
ORDER BY created_at DESC;

-- name: CreateAgentTask :one
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, trigger_comment_id)
VALUES ($1, $2, $3, 'queued', $4, sqlc.narg(trigger_comment_id))
RETURNING *;

-- name: CreateRetryTask :one
-- Clones a parent task into a fresh queued attempt. Carries forward the
-- agent's resume context (session_id/work_dir) so the child can continue
-- the conversation when the backend supports it. attempt is incremented;
-- max_attempts and trigger_comment_id are inherited.
INSERT INTO agent_task_queue (
    agent_id, runtime_id, issue_id, chat_session_id, autopilot_run_id,
    status, priority, trigger_comment_id, context,
    session_id, work_dir,
    attempt, max_attempts, parent_task_id
)
SELECT
    p.agent_id, p.runtime_id, p.issue_id, p.chat_session_id, p.autopilot_run_id,
    'queued', p.priority, p.trigger_comment_id, p.context,
    p.session_id, p.work_dir,
    p.attempt + 1, p.max_attempts, p.id
FROM agent_task_queue p
WHERE p.id = $1
RETURNING *;

-- name: CancelAgentTasksByIssue :many
-- Cancels every active task on the issue and returns the affected rows so the
-- caller can reconcile each agent's status and broadcast task:cancelled events
-- (#1587). Prior :exec form silently dropped that info, so internal cancel
-- paths (issue status flips to cancelled/done, etc.) left agents stuck at
-- status="working" with no self-correction.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running')
RETURNING *;

-- name: CancelAgentTasksByIssueAndAgent :many
-- Cancels active tasks for a single (issue, agent) pair without touching
-- tasks belonging to other agents on the same issue. Used by the manual
-- rerun flow so re-running the assignee doesn't collateral-cancel a
-- still-running @-mention agent on the same issue.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched', 'running')
RETURNING *;

-- name: CancelAgentTasksByAgent :exec
UPDATE agent_task_queue
SET status = 'cancelled'
WHERE agent_id = $1 AND status IN ('queued', 'dispatched', 'running');

-- name: GetAgentTask :one
SELECT * FROM agent_task_queue
WHERE id = $1;

-- name: ClaimAgentTask :one
-- Claims the next queued task for an agent, enforcing per-(issue, agent) serialization:
-- a task is only claimable when no other task for the same issue AND same agent is
-- already dispatched or running. This allows different agents to work on the same
-- issue in parallel while preventing a single agent from running duplicate tasks.
-- Chat tasks (issue_id IS NULL) use chat_session_id for serialization instead.
UPDATE agent_task_queue
SET status = 'dispatched', dispatched_at = now()
WHERE id = (
    SELECT atq.id FROM agent_task_queue atq
    WHERE atq.agent_id = $1 AND atq.status = 'queued'
      AND NOT EXISTS (
          SELECT 1 FROM agent_task_queue active
          WHERE active.agent_id = atq.agent_id
            AND active.status IN ('dispatched', 'running')
            AND (
              (atq.issue_id IS NOT NULL AND active.issue_id = atq.issue_id)
              OR (atq.chat_session_id IS NOT NULL AND active.chat_session_id = atq.chat_session_id)
            )
      )
    ORDER BY atq.priority DESC, atq.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: StartAgentTask :one
UPDATE agent_task_queue
SET status = 'running', started_at = now()
WHERE id = $1 AND status = 'dispatched'
RETURNING *;

-- name: CompleteAgentTask :one
UPDATE agent_task_queue
SET status = 'completed', completed_at = now(), result = $2, session_id = $3, work_dir = $4
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: GetLastTaskSession :one
-- Returns the session_id and work_dir from the most recent task for a given
-- (agent_id, issue_id) pair, used for session resumption. We accept both
-- 'completed' and 'failed' tasks: a failed task may have established a real
-- agent session before crashing (orphaned by a daemon restart, runtime offline,
-- or sweeper timeout), and the daemon pins the resume pointer mid-flight via
-- UpdateAgentTaskSession. Without this, an auto-retry / manual rerun of a
-- mid-run failure would silently start a fresh conversation and lose the
-- in-flight context — exactly what MUL-1128's B branch is meant to fix.
SELECT session_id, work_dir FROM agent_task_queue
WHERE agent_id = $1 AND issue_id = $2
  AND status IN ('completed', 'failed')
  AND session_id IS NOT NULL
ORDER BY COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
LIMIT 1;

-- name: FailAgentTask :one
-- Marks a task as failed. session_id and work_dir are merged via COALESCE so
-- if the agent already established a real session before failing (e.g. it
-- crashed mid-conversation, was cancelled, or hit a tool error) the resume
-- pointer is preserved on the task row. The next chat task can then fall
-- back to GetLastChatTaskSession and continue the conversation instead of
-- silently starting over.
--
-- failure_reason is a coarse classifier consumed by the auto-retry path;
-- 'agent_error' is the safe default when the daemon doesn't supply one.
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = $2,
    failure_reason = COALESCE(sqlc.narg('failure_reason'), 'agent_error'),
    session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir = COALESCE(sqlc.narg('work_dir'), work_dir)
WHERE id = $1 AND status IN ('dispatched', 'running')
RETURNING *;

-- name: UpdateAgentTaskSession :exec
-- Pins the resume pointer mid-flight so a daemon crash leaves a usable
-- session_id/work_dir on the task row. No-op if the task is no longer
-- in dispatched/running.
UPDATE agent_task_queue
SET session_id = COALESCE(sqlc.narg('session_id'), session_id),
    work_dir  = COALESCE(sqlc.narg('work_dir'), work_dir),
    last_heartbeat_at = now()
WHERE id = $1 AND status IN ('dispatched', 'running');

-- name: RecoverOrphanedTasksForRuntime :many
-- Called by the daemon at startup. Atomically fails any dispatched/running
-- task that the prior incarnation of this runtime owned but did not
-- finalize. Returns the failed rows so callers can hand them to the
-- auto-retry path.
UPDATE agent_task_queue
SET status = 'failed',
    completed_at = now(),
    error = 'daemon restarted while task was in flight',
    failure_reason = 'runtime_recovery'
WHERE runtime_id = $1 AND status IN ('dispatched', 'running')
RETURNING *;

-- name: FailStaleTasks :many
-- Fails tasks stuck in dispatched/running beyond the given thresholds.
-- Handles cases where the daemon is alive but the task is orphaned
-- (e.g. agent process hung, daemon failed to report completion).
UPDATE agent_task_queue
SET status = 'failed', completed_at = now(), error = 'task timed out',
    failure_reason = 'timeout'
WHERE (status = 'dispatched' AND dispatched_at < now() - make_interval(secs => @dispatch_timeout_secs::double precision))
   OR (status = 'running' AND started_at < now() - make_interval(secs => @running_timeout_secs::double precision))
RETURNING *;

-- name: CancelAgentTask :one
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now()
WHERE id = $1 AND status IN ('queued', 'dispatched', 'running')
RETURNING *;

-- name: CountRunningTasks :one
SELECT count(*) FROM agent_task_queue
WHERE agent_id = $1 AND status IN ('dispatched', 'running');

-- name: HasActiveTaskForIssue :one
-- Returns true if there is any queued, dispatched, or running task for the issue.
SELECT count(*) > 0 AS has_active FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'running');

-- name: HasPendingTaskForIssue :one
-- Returns true if there is a queued or dispatched (but not yet running) task for the issue.
-- Used by the coalescing queue: allow enqueue when a task is running (so
-- the agent picks up new comments on the next cycle) but skip if a pending
-- task already exists (natural dedup).
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('queued', 'dispatched');

-- name: HasPendingTaskForIssueAndAgent :one
-- Returns true if a specific agent already has a queued or dispatched task
-- for the given issue. Used by @mention trigger dedup.
SELECT count(*) > 0 AS has_pending FROM agent_task_queue
WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched');

-- name: ListPendingTasksByRuntime :many
SELECT * FROM agent_task_queue
WHERE runtime_id = $1 AND status IN ('queued', 'dispatched')
ORDER BY priority DESC, created_at ASC;

-- name: ListActiveTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1 AND status IN ('dispatched', 'running')
ORDER BY created_at DESC;

-- name: ListTasksByIssue :many
SELECT * FROM agent_task_queue
WHERE issue_id = $1
ORDER BY created_at DESC;

-- name: UpdateAgentStatus :one
UPDATE agent SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;
