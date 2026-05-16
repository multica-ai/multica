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

-- name: SetInboxMutedUntilByIssue :execrows
-- Mute every sibling inbox item for the same recipient + issue. The inbox UI
-- deduplicates rows by issue_id, so muting only the clicked notification can
-- make an unmuted sibling resurface after refetch.
UPDATE inbox_item
SET muted_until = $5
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: ClearInboxMute :one
-- Un-mute an inbox item by clearing the muted_until column.
UPDATE inbox_item
SET muted_until = NULL
WHERE id = $1
RETURNING *;

-- name: ClearInboxMuteByIssue :execrows
-- Unmute every sibling inbox item for the same recipient + issue.
UPDATE inbox_item
SET muted_until = NULL
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: SetInboxUnread :one
-- Force an inbox item back to unread state. Used by the "Marker ulæst" action;
-- the value persists until the user explicitly opens the item again.
UPDATE inbox_item
SET read = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxItem :one
-- Restore an archived inbox item to the active inbox. Mirror of upstream
-- ArchiveInboxItem; lives in cerebrodb so we can ship the cerebro
-- archived-view unarchive action without touching upstream SQL.
UPDATE inbox_item
SET archived = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxByIssue :execrows
-- Restore every archived inbox_item for (recipient, issue) — mirrors
-- ArchiveInboxByIssue so issue-level unarchive surfaces every notification
-- the user had for that issue, not only the one row they clicked.
UPDATE inbox_item
SET archived = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = true;
