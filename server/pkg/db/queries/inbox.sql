-- CEREBRO-PATCH(sqlc-inbox): cerebro modification of upstream file
-- name: ListInboxItems :many
-- Generic non-archived listing across both routes. Used only by tests today;
-- production paths use the route-specific ListInboxFeed (route='inbox')
-- and ListNotificationsItems (route='notifications') queries.
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = false
-- CEREBRO-PATCH(sqlc-inbox-remind-sort): resurfaced reminders sort by muted_until so they re-enter the feed at the planned time.
ORDER BY CASE
  WHEN i.muted_until IS NOT NULL AND i.muted_until <= NOW() THEN i.muted_until
  ELSE i.created_at
END DESC;

-- name: ListInboxFeed :many
-- Inbox-routed items (route='inbox') for a user, not archived. Backs the
-- default inbox view.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id,
       iss.parent_issue_id as parent_issue_id, -- CEREBRO-PATCH(inbox-parent-grouping): backs the "Parent issue" group-by
       parent.title as parent_issue_title -- CEREBRO-PATCH(inbox-parent-grouping)
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
LEFT JOIN issue parent ON parent.id = iss.parent_issue_id -- CEREBRO-PATCH(inbox-parent-grouping)
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND i.route = 'inbox'
-- CEREBRO-PATCH(sqlc-inbox-remind-sort): resurfaced reminders sort by muted_until so they re-enter the feed at the planned time.
ORDER BY CASE
  WHEN i.muted_until IS NOT NULL AND i.muted_until <= NOW() THEN i.muted_until
  ELSE i.created_at
END DESC;

-- name: ListArchivedInboxFeed :many
-- Archived inbox-routed items for a user. Backs the "show archived" view in
-- the inbox kebab menu. CEREBRO-PATCH(inbox-archive-pagination): page the
-- archived view so it does not load the full archive on first render.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id,
       iss.parent_issue_id as parent_issue_id, -- CEREBRO-PATCH(inbox-parent-grouping): backs the "Parent issue" group-by
       parent.title as parent_issue_title -- CEREBRO-PATCH(inbox-parent-grouping)
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
LEFT JOIN issue parent ON parent.id = iss.parent_issue_id -- CEREBRO-PATCH(inbox-parent-grouping)
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = true
  AND i.route = 'inbox'
-- CEREBRO-PATCH(inbox-archived-at-sort): FIR-1686 — order by archive time so the archived block defaults to most-recently-archived first.
ORDER BY i.archived_at DESC NULLS LAST, i.created_at DESC
LIMIT $4 OFFSET $5;

-- name: ListNotificationsItems :many
-- Notifications-routed items (route='notifications') that are not archived.
-- Mirrors ListInboxFeed but for the lighter-weight notifications page.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id,
       iss.parent_issue_id as parent_issue_id, -- CEREBRO-PATCH(inbox-parent-grouping): backs the "Parent issue" group-by
       parent.title as parent_issue_title -- CEREBRO-PATCH(inbox-parent-grouping)
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
LEFT JOIN issue parent ON parent.id = iss.parent_issue_id -- CEREBRO-PATCH(inbox-parent-grouping)
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND i.route = 'notifications'
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

-- name: ClaimDueReminderInboxItems :many
-- CEREBRO-PATCH(inbox-reminders-due): atomically claim reminders whose
-- muted_until has passed, force them unread, and stamp due_notified_at so
-- the live/push sweeper only rings once.
UPDATE inbox_item
SET read = false,
    details = COALESCE(details, '{}'::jsonb) || jsonb_build_object('due_notified_at', NOW(), 'due_pending', false)
WHERE id IN (
  SELECT id
  FROM inbox_item
  WHERE type = 'reminder'
    AND route = 'inbox'
    AND archived = false
    AND muted_until IS NOT NULL
    AND muted_until <= NOW()
    AND details->>'due_pending' = 'true'
    AND COALESCE(details->>'due_notified_at', '') = ''
  ORDER BY muted_until ASC
  LIMIT $1
  FOR UPDATE SKIP LOCKED
)
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

-- name: MarkInboxReadByIssue :execrows
-- Marks every unread, non-archived inbox_item for (recipient, issue) as read,
-- regardless of route. Drives channel/DM auto-mark-read so notifications-routed
-- rows are cleared too — without this they keep the channel's unread badge lit
-- because CountUnreadInboxForChannel sums across both routes.
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4
  AND read = false AND archived = false;

-- CEREBRO-PATCH(inbox-thread-split-markread): FIR-1854 — mark a channel/DM read
-- WITHOUT touching replies that live in a thread (details.thread_root_id set),
-- so opening the conversation leaves unread thread rows intact (Slack-style
-- independent thread read state).
-- name: MarkInboxReadByIssueExcludingThreads :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4
  AND read = false AND archived = false
  AND (details->>'thread_root_id') IS NULL;

-- CEREBRO-PATCH(inbox-thread-split-threadread): FIR-1854 — mark only the inbox
-- rows belonging to one thread (matched on details.thread_root_id) read, so
-- opening a thread clears just that thread and not the rest of the channel.
-- name: MarkInboxReadByThreadRoot :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4
  AND (details->>'thread_root_id') = sqlc.arg(thread_root_id)::text
  AND read = false AND archived = false;

-- name: ArchiveInboxByIssueAndType :many
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND issue_id = $2 AND type = $3 AND archived = false
RETURNING recipient_type, recipient_id;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item
WHERE inbox_item.workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3
  AND read = false AND archived = false AND route = 'inbox'
  -- CEREBRO-PATCH(sqlc-inbox): muted items don't contribute to the unread badge.
  AND (muted_until IS NULL OR muted_until <= NOW())
  -- CEREBRO-PATCH(rounds-badge-exclusion): FIR-3114 — issues in the recipient's rounds notify only inside the round, never in count badges.
  AND NOT EXISTS (SELECT 1 FROM cerebro_round_member crm JOIN cerebro_round cr ON cr.id = crm.round_id WHERE crm.issue_id = inbox_item.issue_id AND cr.owner_id = inbox_item.recipient_id);

-- name: CountUnreadInboxForUserAllWorkspaces :one
-- Number of unread inbox "threads" for a member across every workspace.
-- Drives the OS-level PWA app badge.
--
-- Inbox items are exposed in the UI grouped by issue_id (one row per issue,
-- Linear-style — see deduplicateInboxItems on the frontend). A single issue
-- can carry several inbox_item rows for the same recipient (assignment +
-- comment + status change), but the user only sees one entry. Counting raw
-- rows here made the badge run several times higher than the sidebar — this
-- is the JEH-561 follow-up fix.
--
-- The dedup mirrors the frontend exactly: group by COALESCE(issue_id, id),
-- pick the latest row by created_at within each group, count those whose
-- 'read' flag is false. recipient_type is fixed to 'member' — agents have
-- no OS badge.
-- CEREBRO-PATCH(badge-reminder-standalone): group reminders by their own id (not issue_id) so the OS badge matches the frontend, where a fired reminder is a standalone row neither folded into nor hidden by its issue's other rows (FIR-2278).
SELECT count(*) FROM (
    SELECT DISTINCT ON (CASE WHEN type = 'reminder' THEN id ELSE COALESCE(issue_id, id) END) read, muted_until
    FROM inbox_item
    WHERE recipient_type = 'member' AND recipient_id = $1
      AND archived = false AND route = 'inbox'
      -- CEREBRO-PATCH(rounds-badge-exclusion): FIR-3114 — issues in the recipient's rounds notify only inside the round, never in the OS badge.
      AND NOT EXISTS (SELECT 1 FROM cerebro_round_member crm JOIN cerebro_round cr ON cr.id = crm.round_id WHERE crm.issue_id = inbox_item.issue_id AND cr.owner_id = inbox_item.recipient_id)
    ORDER BY CASE WHEN type = 'reminder' THEN id ELSE COALESCE(issue_id, id) END, created_at DESC
) latest
WHERE read = false
  -- CEREBRO-PATCH(sqlc-inbox): muted items don't contribute to the OS badge.
  AND (muted_until IS NULL OR muted_until <= NOW());

-- name: CountUnreadNotifications :one
SELECT count(*) FROM inbox_item
WHERE inbox_item.workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3
  AND read = false AND archived = false AND route = 'notifications'
  -- CEREBRO-PATCH(sqlc-inbox): muted items don't contribute to the unread badge.
  AND (muted_until IS NULL OR muted_until <= NOW())
  -- CEREBRO-PATCH(rounds-badge-exclusion): FIR-3114 — issues in the recipient's rounds notify only inside the round, never in count badges.
  AND NOT EXISTS (SELECT 1 FROM cerebro_round_member crm JOIN cerebro_round cr ON cr.id = crm.round_id WHERE crm.issue_id = inbox_item.issue_id AND cr.owner_id = inbox_item.recipient_id);

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
