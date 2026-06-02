-- name: ListInboxItems :many
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND (
      i.triage_status = 'pending'
      OR (i.triage_status = 'snoozed' AND i.snoozed_until <= now())
  )
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

-- name: HandleInboxItem :one
UPDATE inbox_item SET
    triage_status = 'handled',
    handled_at = now(),
    dismissed_at = NULL,
    snoozed_until = NULL,
    triaged_by = $2,
    read = true
WHERE id = $1
RETURNING *;

-- name: HandleInboxByIssue :execrows
UPDATE inbox_item SET
    triage_status = 'handled',
    handled_at = now(),
    dismissed_at = NULL,
    snoozed_until = NULL,
    triaged_by = $4,
    read = true
WHERE workspace_id = $1
  AND recipient_type = $2
  AND recipient_id = $3
  AND issue_id = $5
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');

-- name: DismissInboxItem :one
UPDATE inbox_item SET
    triage_status = 'dismissed',
    dismissed_at = now(),
    handled_at = NULL,
    snoozed_until = NULL,
    triaged_by = $2,
    read = true
WHERE id = $1
RETURNING *;

-- name: DismissInboxByIssue :execrows
UPDATE inbox_item SET
    triage_status = 'dismissed',
    dismissed_at = now(),
    handled_at = NULL,
    snoozed_until = NULL,
    triaged_by = $4,
    read = true
WHERE workspace_id = $1
  AND recipient_type = $2
  AND recipient_id = $3
  AND issue_id = $5
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');

-- name: DismissInboxByIssueInWorkspace :execrows
UPDATE inbox_item SET
    triage_status = 'dismissed',
    dismissed_at = now(),
    handled_at = NULL,
    snoozed_until = NULL,
    triaged_by = $3,
    read = true
WHERE workspace_id = $1
  AND issue_id = $2
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');

-- name: SnoozeInboxItem :one
UPDATE inbox_item SET
    triage_status = 'snoozed',
    snoozed_until = $2,
    handled_at = NULL,
    dismissed_at = NULL,
    triaged_by = $3,
    read = true
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
WHERE workspace_id = $1
  AND recipient_type = $2
  AND recipient_id = $3
  AND read = false
  AND archived = false
  AND (
      triage_status = 'pending'
      OR (triage_status = 'snoozed' AND snoozed_until <= now())
  );

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false;

-- name: ArchiveAllReadInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND read = true AND archived = false;

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.issue_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'));

-- name: HandleCompletedInbox :execrows
UPDATE inbox_item i SET
    triage_status = 'handled',
    handled_at = now(),
    dismissed_at = NULL,
    snoozed_until = NULL,
    triaged_by = $3,
    read = true
WHERE i.workspace_id = $1
  AND i.recipient_type = 'member'
  AND i.recipient_id = $2
  AND i.archived = false
  AND i.triage_status IN ('pending', 'snoozed')
  AND i.issue_id IN (
      SELECT id FROM issue
      WHERE workspace_id = $1
        AND status IN ('done', 'cancelled')
        AND archived_at IS NULL
  );

-- name: BatchHandleInbox :execrows
UPDATE inbox_item SET
    triage_status = 'handled',
    handled_at = now(),
    dismissed_at = NULL,
    snoozed_until = NULL,
    triaged_by = $3,
    read = true
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');

-- name: BatchDismissInbox :execrows
UPDATE inbox_item SET
    triage_status = 'dismissed',
    dismissed_at = now(),
    handled_at = NULL,
    snoozed_until = NULL,
    triaged_by = $3,
    read = true
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');

-- name: BatchSnoozeInbox :execrows
UPDATE inbox_item SET
    triage_status = 'snoozed',
    snoozed_until = $3,
    handled_at = NULL,
    dismissed_at = NULL,
    triaged_by = $4,
    read = true
WHERE workspace_id = $1
  AND recipient_type = 'member'
  AND recipient_id = $2
  AND archived = false
  AND triage_status IN ('pending', 'snoozed');
