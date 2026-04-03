-- name: ListInboxItems :many
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = ? AND i.recipient_type = ? AND i.recipient_id = ? AND i.archived = 0
ORDER BY i.created_at DESC;

-- name: GetInboxItem :one
SELECT * FROM inbox_item
WHERE id = ?;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = ? AND workspace_id = ?;

-- name: CreateInboxItem :one
INSERT INTO inbox_item (
    id, workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: MarkInboxRead :one
UPDATE inbox_item SET read = 1
WHERE id = ?
RETURNING *;

-- name: ArchiveInboxItem :one
UPDATE inbox_item SET archived = 1
WHERE id = ?
RETURNING *;

-- name: ArchiveInboxByIssue :execrows
UPDATE inbox_item SET archived = 1
WHERE workspace_id = ? AND recipient_type = ? AND recipient_id = ? AND issue_id = ? AND archived = 0;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item
WHERE workspace_id = ? AND recipient_type = ? AND recipient_id = ? AND read = 0 AND archived = 0;

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = 1
WHERE workspace_id = ? AND recipient_type = 'member' AND recipient_id = ? AND archived = 0 AND read = 0;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = 1
WHERE workspace_id = ? AND recipient_type = 'member' AND recipient_id = ? AND archived = 0;

-- name: ArchiveAllReadInbox :execrows
UPDATE inbox_item SET archived = 1
WHERE workspace_id = ? AND recipient_type = 'member' AND recipient_id = ? AND read = 1 AND archived = 0;

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item SET archived = 1
WHERE inbox_item.workspace_id = ? AND inbox_item.recipient_type = 'member' AND inbox_item.recipient_id = ? AND inbox_item.archived = 0
  AND inbox_item.issue_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'));
