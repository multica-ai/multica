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
  AND i.kind IN ('channel', 'dm')
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
