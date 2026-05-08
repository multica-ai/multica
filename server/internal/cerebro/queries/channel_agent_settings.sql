-- name: GetChannelAgentListenMode :one
SELECT listen_mode FROM cerebro_channel_agent_settings
WHERE channel_id = $1 AND agent_id = $2;

-- name: ListChannelAgentListenModes :many
SELECT agent_id, listen_mode FROM cerebro_channel_agent_settings
WHERE channel_id = $1;

-- name: UpsertChannelAgentListenMode :exec
INSERT INTO cerebro_channel_agent_settings (channel_id, agent_id, listen_mode)
VALUES ($1, $2, $3)
ON CONFLICT (channel_id, agent_id) DO UPDATE
SET listen_mode = EXCLUDED.listen_mode, updated_at = NOW();

-- name: DeleteChannelAgentListenMode :exec
DELETE FROM cerebro_channel_agent_settings
WHERE channel_id = $1 AND agent_id = $2;
