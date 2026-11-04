-- name: RecordJobOutcome :exec
-- One job's finished plan, written by the scheduler. A success keeps the failure
-- timestamps; a failure keeps the success timestamps.
INSERT INTO job_status (job_name, last_run_at, last_success_at, last_failed_at, last_error, updated_at)
VALUES (sqlc.arg('job_name')::text, sqlc.arg('at')::timestamptz,
        CASE WHEN sqlc.arg('succeeded')::boolean THEN sqlc.arg('at')::timestamptz END,
        CASE WHEN NOT sqlc.arg('succeeded')::boolean THEN sqlc.arg('at')::timestamptz END,
        CASE WHEN sqlc.arg('succeeded')::boolean THEN '' ELSE sqlc.arg('error_msg')::text END,
        sqlc.arg('at')::timestamptz)
ON CONFLICT (job_name) DO UPDATE SET
    last_run_at = EXCLUDED.last_run_at,
    last_success_at = CASE WHEN sqlc.arg('succeeded')::boolean THEN EXCLUDED.last_success_at ELSE job_status.last_success_at END,
    last_failed_at = CASE WHEN NOT sqlc.arg('succeeded')::boolean THEN EXCLUDED.last_failed_at ELSE job_status.last_failed_at END,
    last_error = EXCLUDED.last_error,
    updated_at = EXCLUDED.updated_at;

-- name: ListJobStatuses :many
SELECT job_name, last_run_at, last_success_at, last_failed_at
FROM job_status
WHERE job_name = ANY(sqlc.arg('job_names')::text[]);
