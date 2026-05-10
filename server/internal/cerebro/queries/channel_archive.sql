-- name: ArchiveChannelForUser :exec
INSERT INTO cerebro_channel_archived (channel_id, user_id)
VALUES ($1, $2)
ON CONFLICT (channel_id, user_id) DO UPDATE
SET archived_at = NOW();

-- name: UnarchiveChannelForUser :exec
DELETE FROM cerebro_channel_archived
WHERE channel_id = $1 AND user_id = $2;

-- name: ListArchivedChannelsForUser :many
SELECT channel_id, archived_at
FROM cerebro_channel_archived
WHERE user_id = $1
ORDER BY archived_at DESC;

-- name: IsChannelArchivedForUser :one
SELECT EXISTS (
    SELECT 1 FROM cerebro_channel_archived
    WHERE channel_id = $1 AND user_id = $2
);

-- name: GetChannelArchivedAtForUser :one
SELECT archived_at FROM cerebro_channel_archived
WHERE channel_id = $1 AND user_id = $2;
