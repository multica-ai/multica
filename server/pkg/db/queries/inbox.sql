-- name: ListInboxItems :many
-- Generic non-archived listing across both routes. Used only by tests today;
-- production paths use the route-specific ListInboxItemsUnfiled (route='inbox')
-- and ListNotificationsItems (route='notifications') queries.
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = false
ORDER BY i.created_at DESC;

-- name: GetInboxItem :one
SELECT * FROM inbox_item
WHERE id = $1;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = $1 AND workspace_id = $2;

-- name: CreateInboxItem :one
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, route
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: MarkInboxRead :one
UPDATE inbox_item SET read = true
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxItem :one
UPDATE inbox_item SET archived = true
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxByIssue :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3
  AND read = false AND archived = false AND route = 'inbox';

-- name: CountUnreadInboxForUserAllWorkspaces :one
-- Total unread inbox items for a member across every workspace they belong to.
-- Drives the OS-level PWA app badge, which is single-icon and cannot reflect
-- workspace scope. recipient_type is fixed to 'member' because agents do not
-- carry an OS badge.
SELECT count(*) FROM inbox_item
WHERE recipient_type = 'member' AND recipient_id = $1
  AND read = false AND archived = false AND route = 'inbox';

-- name: CountUnreadNotifications :one
SELECT count(*) FROM inbox_item
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3
  AND read = false AND archived = false AND route = 'notifications';

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND archived = false AND read = false AND route = 'inbox';

-- name: MarkAllNotificationsRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND archived = false AND read = false AND route = 'notifications';

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND archived = false AND route = 'inbox';

-- name: ArchiveAllNotifications :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND archived = false AND route = 'notifications';

-- name: ArchiveAllReadInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND read = true AND archived = false AND route = 'inbox';

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2
  AND i.archived = false AND i.route = 'inbox'
  AND i.issue_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'));
