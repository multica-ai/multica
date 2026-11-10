-- name: ListOpenIssueEnvironmentsByTask :many
SELECT * FROM issue_environment
WHERE task_id = $1 AND ended_at IS NULL;

-- name: UpsertIssueEnvironment :one
-- A reported environment that has exited or stopped keeps its first ended_at,
-- so re-reporting the same snapshot does not move it.
INSERT INTO issue_environment (
    workspace_id, issue_id, task_id, runtime_id, agent_id, local_key, source,
    kind, name, label, url, port, state, detail, started_at, expires_at, ended_at
) VALUES (
    @workspace_id, @issue_id, @task_id, @runtime_id, @agent_id, @local_key, @source,
    @kind, @name, @label, @url, @port, @state, @detail, @started_at, @expires_at,
    CASE WHEN @state::text IN ('exited', 'stopped') THEN now() END
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
