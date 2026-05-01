-- name: CreateModelListRequest :one
INSERT INTO model_list_request (id, runtime_id, status, supported, created_at, updated_at)
VALUES ($1, $2, 'pending', true, now(), now())
RETURNING *;

-- name: GetModelListRequest :one
SELECT * FROM model_list_request
WHERE id = $1;

-- name: PopPendingModelListRequest :one
-- Atomically claim the oldest pending request for a runtime.
-- FOR UPDATE SKIP LOCKED ensures correctness across concurrent server instances.
UPDATE model_list_request
SET status = 'running', updated_at = now()
WHERE id = (
    SELECT mlr.id FROM model_list_request mlr
    WHERE mlr.runtime_id = $1 AND mlr.status = 'pending'
    ORDER BY mlr.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteModelListRequest :exec
UPDATE model_list_request
SET status = 'completed', models = $2, supported = $3, updated_at = now()
WHERE id = $1;

-- name: FailModelListRequest :exec
UPDATE model_list_request
SET status = 'failed', error = $2, updated_at = now()
WHERE id = $1;

-- name: TimeoutModelListRequest :exec
UPDATE model_list_request
SET status = 'timeout', error = $2, updated_at = now()
WHERE id = $1;

-- name: DeleteStaleModelListRequests :exec
DELETE FROM model_list_request
WHERE created_at < now() - interval '2 minutes';
