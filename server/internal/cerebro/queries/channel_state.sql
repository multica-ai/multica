-- Per-(channel × user) "remind me" (snooze) + "mark as unread" state for
-- channels/DMs. See migration 9072_cerebro_channel_state.

-- name: SetChannelMutedForUser :exec
INSERT INTO cerebro_channel_state (channel_id, user_id, muted_until, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (channel_id, user_id) DO UPDATE
SET muted_until = $3, updated_at = now();

-- name: ClearChannelMuteForUser :exec
UPDATE cerebro_channel_state
SET muted_until = NULL, updated_at = now()
WHERE channel_id = $1 AND user_id = $2;

-- name: SetChannelUnreadForUser :exec
INSERT INTO cerebro_channel_state (channel_id, user_id, unread_at, updated_at)
VALUES ($1, $2, now(), now())
ON CONFLICT (channel_id, user_id) DO UPDATE
SET unread_at = now(), updated_at = now();

-- name: ClearChannelStateForUser :exec
-- Reading a channel clears both the manual-unread overlay and any snooze:
-- the user is looking at it now, so neither control still applies.
UPDATE cerebro_channel_state
SET unread_at = NULL, muted_until = NULL, updated_at = now()
WHERE channel_id = $1 AND user_id = $2;

-- name: ListChannelStatesForUser :many
SELECT channel_id, muted_until, unread_at
FROM cerebro_channel_state
WHERE user_id = $1;

-- name: GetChannelStateForUser :one
SELECT channel_id, muted_until, unread_at
FROM cerebro_channel_state
WHERE channel_id = $1 AND user_id = $2;
