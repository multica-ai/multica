-- Cerebro-only inbox queries. Lives in cerebrodb so upstream merges of
-- pkg/db/queries/inbox.sql don't conflict.
--
-- Note: these queries operate on the upstream `inbox_item` table; cerebro
-- adds columns (route, project_id, muted_until) via 9NNN migrations.

-- name: SetInboxMutedUntil :one
-- Mute an inbox item until the given timestamp (caller computes "next 08:00
-- local" or whatever duration applies). Idempotent — re-muting just updates
-- the timestamp.
UPDATE inbox_item
SET muted_until = $2
WHERE id = $1
RETURNING *;

-- name: ClearInboxMute :one
-- Un-mute an inbox item by clearing the muted_until column.
UPDATE inbox_item
SET muted_until = NULL
WHERE id = $1
RETURNING *;

-- name: SetInboxUnread :one
-- Force an inbox item back to unread state. Used by the "Marker ulæst" action;
-- the value persists until the user explicitly opens the item again.
UPDATE inbox_item
SET read = false
WHERE id = $1
RETURNING *;
