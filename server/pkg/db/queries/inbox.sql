-- name: ListInboxItems :many
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
    actor_type, actor_id, details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
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
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND read = false AND archived = false;

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false;

-- name: ArchiveAllReadInbox :execrows
-- Archive all inbox items belonging to "representatives" whose newest non-archived item
-- is read. A group is an issue (when issue_id is set) or a single standalone
-- item (when issue_id is NULL). This matches the inbox UI's dedup-by-issue
-- semantic: the list shows one row per issue keyed on the newest non-archived
-- inbox_item, so "Archive all read" should archive entire issues that the user
-- sees as read. Per-row archiving (the previous behavior) hid the read row but
-- exposed older unread siblings, flipping the issue from "read" to "unread"
-- in the list (PUL-39).
WITH representatives AS (
  SELECT DISTINCT ON (COALESCE(issue_id, id))
    issue_id,
    id AS representative_id,
    read AS representative_read
  FROM inbox_item
  WHERE workspace_id = $1
    AND recipient_type = 'member'
    AND recipient_id = $2
    AND archived = false
  ORDER BY COALESCE(issue_id, id), created_at DESC
)
UPDATE inbox_item AS i SET archived = true
WHERE i.workspace_id = $1
  AND i.recipient_type = 'member'
  AND i.recipient_id = $2
  AND i.archived = false
  AND (
    (i.issue_id IS NOT NULL AND i.issue_id IN (
      SELECT r.issue_id FROM representatives r WHERE r.representative_read = true AND r.issue_id IS NOT NULL
    ))
    OR (i.issue_id IS NULL AND i.id IN (
      SELECT r.representative_id FROM representatives r WHERE r.representative_read = true AND r.issue_id IS NULL
    ))
  );

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.issue_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'));
