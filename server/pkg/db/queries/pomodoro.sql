-- name: GetPomodoroSession :one
SELECT * FROM pomodoro_sessions
WHERE user_id = $1 AND workspace_id = $2;

-- name: UpsertPomodoroStart :one
INSERT INTO pomodoro_sessions (
    user_id, workspace_id, phase, phase_duration_seconds,
    status, elapsed_seconds, started_at, updated_at
) VALUES (
    $1, $2, 'work', 1500, 'running', 0, NOW(), NOW()
)
ON CONFLICT (user_id, workspace_id) DO UPDATE SET
    status = 'running',
    started_at = NOW(),
    updated_at = NOW()
RETURNING *;

-- name: UpdatePomodoroSession :one
UPDATE pomodoro_sessions SET
    phase = $3,
    phase_duration_seconds = $4,
    status = $5,
    elapsed_seconds = $6,
    started_at = $7,
    updated_at = NOW()
WHERE user_id = $1 AND workspace_id = $2
RETURNING *;

-- name: GetPomodoroHistory :many
SELECT te.id, te.workspace_id, te.user_id, te.issue_id, te.description,
       te.start_time, te.stop_time, te.duration_seconds, te.type, te.created_at
FROM time_entry te
WHERE te.user_id = @user_id
  AND te.workspace_id = @workspace_id
  AND te.type = 'pomodoro'
ORDER BY te.start_time DESC
LIMIT @limit_count
OFFSET @offset_count;

-- name: GetPomodoroStats :one
SELECT
  COUNT(CASE WHEN te.start_time >= @today_start THEN 1 END)::int AS today_count,
  COUNT(CASE WHEN te.start_time >= @week_start THEN 1 END)::int AS week_count,
  COALESCE(SUM(te.duration_seconds), 0)::int AS total_seconds
FROM time_entry te
WHERE te.user_id = @user_id
  AND te.workspace_id = @workspace_id
  AND te.type = 'pomodoro';

-- name: IncrementPomodoroCount :one
UPDATE pomodoro_sessions
SET pomodoro_count = pomodoro_count + 1
WHERE user_id = @user_id
  AND workspace_id = @workspace_id
RETURNING pomodoro_count;
