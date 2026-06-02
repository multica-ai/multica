-- name: CreateFailoverGroup :one
INSERT INTO runtime_failover_group (workspace_id, name, strategy)
VALUES (@workspace_id, @name, @strategy)
RETURNING *;

-- name: GetFailoverGroup :one
SELECT * FROM runtime_failover_group
WHERE id = @id;

-- name: ListFailoverGroups :many
SELECT * FROM runtime_failover_group
WHERE workspace_id = @workspace_id
ORDER BY created_at ASC;

-- name: UpdateFailoverGroup :one
UPDATE runtime_failover_group
SET name = @name, strategy = @strategy, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFailoverGroup :exec
DELETE FROM runtime_failover_group
WHERE id = @id;

-- name: ListRuntimesByFailoverGroup :many
SELECT * FROM agent_runtime
WHERE failover_group_id = @failover_group_id
ORDER BY priority DESC, created_at ASC;
