-- name: GetActiveIssueTimerForActor :one
SELECT * FROM issue_time_entry
WHERE workspace_id = $1
  AND actor_type = $2
  AND actor_id = $3
  AND stopped_at IS NULL
LIMIT 1;

-- name: GetActiveIssueTimerForTask :one
SELECT * FROM issue_time_entry
WHERE task_id = $1
  AND stopped_at IS NULL
LIMIT 1;

-- name: CreateIssueTimeEntry :one
INSERT INTO issue_time_entry (
    workspace_id, issue_id, actor_type, actor_id, task_id, source
) VALUES (
    $1, $2, $3, $4, sqlc.narg('task_id'), $5
)
RETURNING *;

-- name: StopIssueTimeEntry :one
UPDATE issue_time_entry
SET stopped_at = now(),
    updated_at = now()
WHERE id = $1
  AND stopped_at IS NULL
RETURNING *;

-- name: GetIssueTimeSummary :one
SELECT
    COALESCE(SUM(EXTRACT(EPOCH FROM (COALESCE(stopped_at, now()) - started_at))::bigint), 0)::bigint AS total_seconds,
    COUNT(*)::bigint AS entry_count
FROM issue_time_entry
WHERE issue_id = $1;

-- name: GetActiveIssueTimerForIssueAndActor :one
SELECT * FROM issue_time_entry
WHERE issue_id = $1
  AND actor_type = $2
  AND actor_id = $3
  AND stopped_at IS NULL
LIMIT 1;
