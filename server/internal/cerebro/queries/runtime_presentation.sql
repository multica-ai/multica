-- Cerebro: per-runtime presentation mode mutators. The 'presentation_mode'
-- column is owned by cerebro and only consumed by cerebro packages (terminal
-- broker, UI toggle). Upstream code is unaware of the column.

-- name: UpdateAgentRuntimePresentationMode :one
UPDATE agent_runtime
SET presentation_mode = @presentation_mode, updated_at = now()
WHERE id = @id
RETURNING id, presentation_mode;

-- name: GetAgentRuntimePresentationMode :one
SELECT presentation_mode FROM agent_runtime WHERE id = $1;
