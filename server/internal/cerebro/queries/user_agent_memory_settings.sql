-- name: GetUserAgentMemorySettings :one
SELECT can_read_memory, can_write_memory
FROM cerebro_user_agent_memory_settings
WHERE user_id = $1 AND agent_id = $2;

-- name: UpsertUserAgentMemorySettings :one
INSERT INTO cerebro_user_agent_memory_settings (user_id, agent_id, can_read_memory, can_write_memory)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, agent_id) DO UPDATE
SET can_read_memory = EXCLUDED.can_read_memory,
    can_write_memory = EXCLUDED.can_write_memory,
    updated_at = NOW()
RETURNING can_read_memory, can_write_memory;

-- name: DeleteUserAgentMemorySettings :exec
DELETE FROM cerebro_user_agent_memory_settings
WHERE user_id = $1 AND agent_id = $2;
