-- TECH-3352 — per-chat-session snooze ("remind me"). Mark-as-unread reuses the
-- upstream chat_session.unread_since machinery (SetUnreadSinceIfNull), so only
-- the snooze + the per-user muted lookup live here.

-- name: SetChatSessionMuted :exec
UPDATE chat_session SET muted_until = $3 WHERE id = $1 AND creator_id = $2;

-- name: ClearChatSessionMuted :exec
UPDATE chat_session SET muted_until = NULL WHERE id = $1 AND creator_id = $2;

-- name: ListChatMutedForUser :many
SELECT id, muted_until FROM chat_session
WHERE creator_id = $1 AND workspace_id = $2 AND muted_until IS NOT NULL;
