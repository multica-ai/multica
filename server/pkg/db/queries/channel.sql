-- Channels are issues with kind IN ('channel', 'dm'). The issue table is
-- the storage; these queries narrow the view so the channel UI doesn't
-- have to know they share a table with tasks.

-- name: ListChannelsForUser :many
-- Returns channels (kind != 'issue') where the user is a subscriber. Joins
-- through issue_subscriber so participation is the gating predicate.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.kind, i.assignee_type, i.assignee_id,
       i.creator_type, i.creator_id, i.project_id,
       i.created_at, i.updated_at, i.number
FROM issue i
JOIN issue_subscriber s ON s.issue_id = i.id
WHERE i.workspace_id = $1
  AND i.kind IN ('channel', 'dm', 'group') -- CEREBRO-PATCH(channel-group-kind): FIR-2159
  AND s.user_type = 'member'
  AND s.user_id = $2
ORDER BY i.updated_at DESC;

-- name: ListLatestCommentsForIssues :many
-- Returns the most recent user comment per issue from a list of issue IDs.
-- DISTINCT ON keeps a single row per issue — the latest one. Used by the
-- inbox to render a "Sara: shipping at 3" preview under each channel row
-- without doing N round-trips.
SELECT DISTINCT ON (c.issue_id)
       c.issue_id, c.author_type, c.author_id, c.content, c.created_at
FROM comment c
WHERE c.issue_id = ANY($1::uuid[])
  AND c.type = 'comment'
ORDER BY c.issue_id, c.created_at DESC;

-- name: GetDMByMembers :one
-- Find an existing DM between exactly two members. Used to make DM
-- creation idempotent — opening "DM with Sara" twice returns the same
-- channel rather than creating a duplicate. The query checks that:
--   1. The issue is kind='dm'
--   2. Both members are subscribers
--   3. No other members are subscribers
SELECT i.* FROM issue i
WHERE i.workspace_id = $1
  AND i.kind = 'dm'
  AND EXISTS (
      SELECT 1 FROM issue_subscriber s
      WHERE s.issue_id = i.id AND s.user_type = 'member' AND s.user_id = $2
  )
  AND EXISTS (
      SELECT 1 FROM issue_subscriber s
      WHERE s.issue_id = i.id AND s.user_type = 'member' AND s.user_id = $3
  )
  AND (
      SELECT count(*) FROM issue_subscriber s
      WHERE s.issue_id = i.id AND s.user_type = 'member'
  ) = 2
LIMIT 1;

-- name: CountUnreadInboxForChannel :one
-- Counts unread inbox items targeting a channel for a specific user.
-- Used to drive the unread badge in the channel sidebar.
SELECT count(*) FROM inbox_item
WHERE recipient_type = 'member'
  AND recipient_id = $1
  AND issue_id = $2
  AND read = FALSE
  AND archived = FALSE;

-- CEREBRO-PATCH(inbox-thread-split-unread): FIR-1854 — channel badge that
-- excludes thread replies (details.thread_root_id present). A reply living
-- inside a thread shows on its own split inbox row, not on the channel count.
-- name: CountUnreadInboxForChannelExcludingThreads :one
SELECT count(*) FROM inbox_item
WHERE recipient_type = 'member'
  AND recipient_id = $1
  AND issue_id = $2
  AND read = FALSE
  AND archived = FALSE
  AND (details->>'thread_root_id') IS NULL;

-- CEREBRO-PATCH(sqlc-channel-unread-smart): FIR-2010 — channel badge that only
-- counts messages where the viewer was @mentioned (inbox_item.type =
-- 'mentioned'). Drives the smart channel unread badge: a channel goes red only
-- on a mention, while other activity surfaces as a bold dot. Thread replies
-- (details.thread_root_id present) are excluded so they keep their own row.
-- name: CountUnreadInboxForChannelMentionsOnly :one
SELECT count(*) FROM inbox_item
WHERE recipient_type = 'member'
  AND recipient_id = $1
  AND issue_id = $2
  AND type = 'mentioned'
  AND read = FALSE
  AND archived = FALSE
  AND (details->>'thread_root_id') IS NULL;

-- CEREBRO-PATCH(sqlc-channel-dm-promote): JEH-1131 — DM auto-promotion
-- on third-party mention. The PromoteDMToGroup, ConvertGroupToChannel and
-- ListChannelParticipantNames queries below are cerebro-only; upstream
-- never needs to flip kind on an existing channel-table row.
-- name: PromoteDMToGroup :one
-- FIR-2159. Flips a DM-kind issue to group kind in one statement when a third
-- party joins. The WHERE clause makes it idempotent: a second call (or a call
-- on an issue that is no longer a DM) returns no row, which the service treats
-- as a no-op. Title is only filled when currently empty so a user-set title is
-- preserved. A group keeps its participant-derived title, exactly like a DM.
UPDATE issue
SET kind = 'group',
    title = CASE WHEN title = '' THEN COALESCE(sqlc.narg('title'), '') ELSE title END,
    updated_at = now()
WHERE id = $1
  AND kind = 'dm'
RETURNING *;

-- name: ConvertGroupToChannel :one
-- FIR-2159. The deliberate "Convert to channel" step: flips a group-kind issue
-- to channel kind and sets the user-chosen name. Idempotent — a second call (or
-- a call on an issue that is not a group) returns no row. The name is always
-- written because converting is the moment the conversation earns a fixed name.
UPDATE issue
SET kind = 'channel',
    title = $2,
    updated_at = now()
WHERE id = $1
  AND kind = 'group'
RETURNING *;

-- CEREBRO-PATCH(fir-125-channel-cli): workspace-level channel/DM listing for analytics
-- name: ListAllChannelsInWorkspace :many
-- Returns ALL channels and DMs in the workspace regardless of subscriber.
-- Used by the multica CLI for workspace-wide analytics.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.kind, i.assignee_type, i.assignee_id,
       i.creator_type, i.creator_id, i.project_id,
       i.created_at, i.updated_at, i.number
FROM issue i
WHERE i.workspace_id = $1
  AND i.kind IN ('channel', 'dm', 'group') -- CEREBRO-PATCH(channel-group-kind): FIR-2159
ORDER BY i.updated_at DESC;

-- name: ListChannelParticipantNames :many
-- Returns display names of every (member or agent) subscriber of a channel/dm
-- in subscribed-at order. Used to auto-generate a channel title on DM
-- promotion (JEH-1131); harmless for any other caller that just wants a
-- one-shot name list keyed off issue_subscriber. Workspace ID is required
-- to scope the member-row join — agents are workspace-scoped by their own
-- table.
SELECT u.name AS display_name, s.created_at AS subscribed_at
FROM issue_subscriber s
JOIN member m ON m.user_id = s.user_id AND m.workspace_id = $2
JOIN "user" u ON u.id = m.user_id
WHERE s.issue_id = $1 AND s.user_type = 'member'
UNION ALL
SELECT a.name AS display_name, s.created_at AS subscribed_at
FROM issue_subscriber s
JOIN agent a ON a.id = s.user_id AND a.workspace_id = $2
WHERE s.issue_id = $1 AND s.user_type = 'agent'
ORDER BY subscribed_at ASC;
