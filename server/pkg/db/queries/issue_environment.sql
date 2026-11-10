-- name: ListOpenIssueEnvironmentsByTask :many
SELECT * FROM issue_environment
WHERE task_id = $1 AND ended_at IS NULL;

-- name: UpsertIssueEnvironment :one
-- A reported environment that has exited or stopped keeps its first ended_at,
-- so re-reporting the same snapshot does not move it.
INSERT INTO issue_environment (
    workspace_id, issue_id, task_id, runtime_id, agent_id, local_key, source,
    kind, name, label, url, port, state, detail, started_at, expires_at, ended_at,
    excerpt, controllable
) VALUES (
    @workspace_id, @issue_id, @task_id, @runtime_id, @agent_id, @local_key, @source,
    @kind, @name, @label, @url, @port, @state, @detail, @started_at, @expires_at,
    CASE WHEN @state::text IN ('exited', 'stopped') THEN now() END,
    @excerpt, @controllable
)
ON CONFLICT (task_id, local_key) DO UPDATE SET
    source = EXCLUDED.source,
    kind = EXCLUDED.kind,
    name = EXCLUDED.name,
    label = EXCLUDED.label,
    url = EXCLUDED.url,
    port = EXCLUDED.port,
    state = EXCLUDED.state,
    detail = EXCLUDED.detail,
    excerpt = EXCLUDED.excerpt,
    controllable = EXCLUDED.controllable,
    started_at = EXCLUDED.started_at,
    expires_at = EXCLUDED.expires_at,
    last_seen_at = now(),
    ended_at = CASE
        WHEN EXCLUDED.state IN ('exited', 'stopped') THEN COALESCE(issue_environment.ended_at, now())
    END,
    updated_at = now()
RETURNING *;

-- name: EndIssueEnvironmentsMissingFromSnapshot :exec
-- Environments the daemon no longer reports for the task have stopped. An
-- exited one keeps its state so the sidebar can still say it crashed.
UPDATE issue_environment
SET ended_at = now(),
    state = CASE WHEN state = 'exited' THEN 'exited' ELSE 'stopped' END,
    updated_at = now()
WHERE task_id = @task_id
  AND ended_at IS NULL
  AND NOT (local_key = ANY(@keep_keys::text[]));

-- name: ListIssueEnvironments :many
-- Live environments plus those that ended within the history window.
SELECT * FROM issue_environment
WHERE issue_id = @issue_id
  AND workspace_id = @workspace_id
  AND (ended_at IS NULL OR ended_at > @ended_after::timestamptz)
ORDER BY created_at DESC
LIMIT 50;

-- name: GetIssueEnvironment :one
SELECT * FROM issue_environment
WHERE id = @id AND issue_id = @issue_id AND workspace_id = @workspace_id;

-- name: CreateIssueEnvironmentCommand :one
INSERT INTO issue_environment_command (
    workspace_id, issue_id, runtime_id, environment_id, task_id, action,
    local_key, repo, name, actor_type, actor_id
) VALUES (
    @workspace_id, @issue_id, @runtime_id, @environment_id, @task_id, @action,
    @local_key, @repo, @name, @actor_type, @actor_id
)
RETURNING *;

-- name: HasOpenIssueEnvironmentControl :one
-- A stop or restart still waiting on the daemon for the same environment.
SELECT EXISTS (
    SELECT 1 FROM issue_environment_command
    WHERE issue_id = @issue_id
      AND environment_id = @environment_id
      AND action IN ('stop', 'restart')
      AND (
          (status = 'pending' AND created_at > @pending_after::timestamptz)
          OR (status = 'claimed' AND claimed_at > @claimed_after::timestamptz)
      )
);

-- name: ClaimIssueEnvironmentCommands :many
-- Commands older than the pending window were already reported to the member
-- as unanswered, so they are never carried out late.
UPDATE issue_environment_command
SET status = 'claimed', claimed_at = now()
WHERE id IN (
    SELECT c.id FROM issue_environment_command c
    WHERE c.runtime_id = @runtime_id
      AND c.status = 'pending'
      AND c.created_at > @pending_after::timestamptz
    ORDER BY c.created_at
    LIMIT 20
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteIssueEnvironmentCommand :one
UPDATE issue_environment_command
SET status = @status,
    error = @error,
    output = sqlc.narg('output'),
    result_key = @result_key,
    completed_at = now()
WHERE id = @id AND runtime_id = @runtime_id AND status = 'claimed'
RETURNING *;

-- name: GetIssueEnvironmentCommand :one
SELECT * FROM issue_environment_command
WHERE id = @id AND issue_id = @issue_id AND workspace_id = @workspace_id;

-- name: ListIssueEnvironmentControls :many
-- Recent stops, restarts and starts, without log reads.
SELECT id, workspace_id, issue_id, runtime_id, environment_id, task_id, action,
       local_key, repo, name, actor_type, actor_id, status, error, result_key,
       created_at, claimed_at, completed_at
FROM issue_environment_command
WHERE issue_id = @issue_id
  AND workspace_id = @workspace_id
  AND action <> 'logs'
  AND created_at > @created_after::timestamptz
ORDER BY created_at DESC
LIMIT 100;

-- name: DeleteIssueEnvironmentLogCommandsBefore :execrows
DELETE FROM issue_environment_command
WHERE action = 'logs' AND created_at < @created_before::timestamptz;

-- name: UpsertIssueEnvironmentConfig :exec
INSERT INTO issue_environment_config (task_id, workspace_id, issue_id, runtime_id, environments)
VALUES (@task_id, @workspace_id, @issue_id, @runtime_id, @environments)
ON CONFLICT (task_id) DO UPDATE SET
    runtime_id = EXCLUDED.runtime_id,
    environments = EXCLUDED.environments,
    reported_at = now();

-- name: ListLatestIssueEnvironmentConfigs :many
-- The most recent task on each machine that declared environments.
SELECT DISTINCT ON (runtime_id) *
FROM issue_environment_config
WHERE issue_id = @issue_id AND workspace_id = @workspace_id
ORDER BY runtime_id, reported_at DESC;

-- name: GetIssueEnvironmentConfig :one
SELECT * FROM issue_environment_config
WHERE task_id = @task_id AND issue_id = @issue_id AND workspace_id = @workspace_id;
