-- Inbox v2, group state operations.
--
-- Three callers share these: the v1 write adapters (a legacy endpoint arrives,
-- the group is what actually changes), the v2 endpoints, and reconcile. They
-- are one set on purpose — the whole failure mode this refactor exists to fix
-- is three clients each folding events into a "row" by their own rules, and
-- three server paths each folding state into a group by their own rules would
-- reproduce it one layer down.
--
-- Every one of these must run with the group row locked (AcquireInboxGroup or
-- LockInboxGroup), and the caller must refresh the mirror in the same
-- transaction. Group state that has not reached inbox_item is state the v1
-- clients cannot see.

-- name: LockInboxGroup :one
-- Take the group's row lock without creating anything. The adapters use this
-- rather than AcquireInboxGroup because they operate on a group that must
-- already exist; creating one from a write would resurrect a group the reader
-- had just emptied.
SELECT * FROM inbox_group
WHERE id = @id AND workspace_id = @workspace_id AND recipient_id = @recipient_id
FOR UPDATE;

-- name: FindInboxGroupBySource :one
-- Resolve (workspace, recipient, source) to a group without locking. The v1
-- adapters use it to answer "does this legacy row belong to a group yet".
SELECT * FROM inbox_group
WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id
  AND source_kind = @source_kind AND source_id = @source_id;

-- name: MarkInboxGroupReadThrough :one
-- Advance the read cursor, never retreat it.
--
-- GREATEST, not assignment: two clients can report having seen different
-- events, and a late-arriving read for an older event must not un-read a newer
-- one. This is the "clamp/max merge" rule — the cursor is a high-water mark of
-- what the user has been shown, and high-water marks only go up.
--
-- manual_unread clears only when the caller's observation is current. A user
-- who marks a group unread and then receives a stale automatic read from a tab
-- that had not seen it yet must keep their own decision: same observed_seq,
-- different state_version, and state_version is the tie-break that tells the
-- two apart.
UPDATE inbox_group
SET read_through_seq = GREATEST(read_through_seq, LEAST(@observed_seq, latest_seq)),
    manual_unread    = CASE WHEN state_version = @observed_state_version THEN false ELSE manual_unread END,
    state_version    = state_version + 1,
    updated_at       = @now
WHERE id = @id
RETURNING *;

-- name: MarkInboxGroupUnread :one
-- Explicit user intent. The cursor drops below the head so the group is unread
-- by the ordinary comparison too, and manual_unread records that a human — not
-- a rule — put it there, which is what stops the next automatic read from
-- quietly undoing it.
UPDATE inbox_group
SET manual_unread    = true,
    read_through_seq = LEAST(read_through_seq, latest_seq - 1),
    state_version    = state_version + 1,
    updated_at       = @now
WHERE id = @id
RETURNING *;

-- name: ArchiveInboxGroup :one
-- Archive is "I have dealt with this", so it clears unread as well. A group
-- that came back archived-but-unread would keep a badge lit for work the user
-- has explicitly finished with.
UPDATE inbox_group
SET archived_at      = @now,
    read_through_seq = latest_seq,
    manual_unread    = false,
    state_version    = state_version + 1,
    updated_at       = @now
WHERE id = @id
RETURNING *;

-- name: UnarchiveInboxGroup :one
-- The inverse. read state is left exactly as it was: restoring something the
-- user archived while unread should put the unread back, because the badge
-- only ever counted unarchived groups and the group is now unarchived again.
UPDATE inbox_group
SET archived_at   = NULL,
    surfaced_at   = GREATEST(surfaced_at, latest_event_at),
    state_version = state_version + 1,
    updated_at    = @now
WHERE id = @id
RETURNING *;

-- name: MarkAllInboxGroupsRead :many
-- Batch counterpart of MarkInboxGroupReadThrough, matching v1's MarkAllInboxRead:
-- active groups only, so archived history is not silently touched.
--
-- manual_unread clears here because this IS the user acting — "mark everything
-- read" outranks their earlier per-group unread the same way any later explicit
-- action outranks an earlier one.
UPDATE inbox_group
SET read_through_seq = latest_seq,
    manual_unread    = false,
    state_version    = state_version + 1,
    updated_at       = @now
WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id
  AND archived_at IS NULL
  AND (manual_unread OR read_through_seq < latest_seq)
RETURNING *;

-- name: ArchiveAllInboxGroups :many
UPDATE inbox_group
SET archived_at      = @now,
    read_through_seq = latest_seq,
    manual_unread    = false,
    state_version    = state_version + 1,
    updated_at       = @now
WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveReadInboxGroups :many
-- v1's ArchiveAllReadInbox. "Read" is the derived group predicate, not the
-- stored boolean, so a group the user marked unread by hand survives it.
UPDATE inbox_group
SET archived_at      = @now,
    state_version    = state_version + 1,
    updated_at       = @now
WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id
  AND archived_at IS NULL
  AND NOT (manual_unread OR read_through_seq < latest_seq)
RETURNING *;

-- name: ArchiveCompletedInboxGroups :many
-- v1's ArchiveCompletedInbox, which keys on the ISSUE's status rather than on
-- anything about the notification. Standalone groups have no issue and are
-- therefore never completed — matching v1, where the IN (SELECT id FROM issue)
-- test cannot hold for a NULL issue_id.
UPDATE inbox_group g
SET archived_at      = @now,
    read_through_seq = g.latest_seq,
    manual_unread    = false,
    state_version    = g.state_version + 1,
    updated_at       = @now
WHERE g.workspace_id = @workspace_id AND g.recipient_id = @recipient_id
  AND g.archived_at IS NULL
  AND g.source_kind = 'issue'
  AND g.source_id IN (SELECT id FROM issue WHERE status IN ('done', 'cancelled'))
RETURNING *;

-- name: RefreshInboxItemMirrorForRecipient :execrows
-- Bulk mirror refresh after a batch operation.
--
-- The per-group RefreshInboxItemMirror in one statement over every group a
-- recipient owns in a workspace. Looping the single-group version instead would
-- issue one UPDATE per group inside the request transaction, which for "archive
-- all" on a busy workspace is exactly the unbounded write the batch endpoints
-- are supposed to avoid.
--
-- Same invariant, same IS DISTINCT FROM guard, same dismissal rule as the
-- single-group version — it must be the same expression, or the two paths drift
-- and a group's booleans depend on which endpoint last touched it.
UPDATE inbox_item i
SET read     = CASE WHEN g.unread AND i.event_seq = g.latest_seq THEN false ELSE true END,
    archived = (g.want_archived OR i.dismissed_at IS NOT NULL)
FROM (
    SELECT id,
           (archived_at IS NOT NULL) AS want_archived,
           (manual_unread OR read_through_seq < latest_seq) AS unread,
           latest_seq
    FROM inbox_group
    WHERE inbox_group.workspace_id = @workspace_id AND inbox_group.recipient_id = @recipient_id
) g
WHERE i.group_id = g.id
  AND (
        i.read IS DISTINCT FROM (CASE WHEN g.unread AND i.event_seq = g.latest_seq THEN false ELSE true END)
     OR i.archived IS DISTINCT FROM (g.want_archived OR i.dismissed_at IS NOT NULL)
      );
