-- Inbox v2 read path and reconcile.
--
-- The read side never touches inbox_item's booleans. It renders the group and
-- joins the representative EVENT for its text — which is the whole point of the
-- refactor: "unread" is a comparison between two numbers on one row, not a
-- boolean scattered across every event the group ever collected.

-- name: ListInboxGroupsForRecipient :many
-- The main v2 list. Keyset pagination on (surfaced_at, id), matching
-- inbox_group_recipient_surfaced_idx exactly so the sort is an index walk and
-- not a sort of the whole inbox.
--
-- surfaced_at rather than latest_event_at: a snooze expiring has to bring a
-- group back at the right position with no new event to sort by, and a sort key
-- that only moves when an event arrives would silently return it below
-- everything newer.
--
-- LEFT JOIN issue, not INNER: a deleted issue must not make the notification
-- about it disappear mid-list and take the page's keyset with it. Standalone
-- groups have no issue at all.
--
-- Groups with no representative are skipped. That state is reachable — every
-- event dismissed — and such a group has nothing to render.
SELECT g.*,
       i.type          AS event_type,
       i.severity      AS event_severity,
       i.title         AS event_title,
       i.body          AS event_body,
       i.actor_type    AS event_actor_type,
       i.actor_id      AS event_actor_id,
       i.details       AS event_details,
       i.issue_id      AS event_issue_id,
       i.target_kind   AS event_target_kind,
       i.target_id     AS event_target_id,
       i.created_at    AS event_created_at,
       iss.status      AS issue_status,
       (g.manual_unread OR g.read_through_seq < g.latest_seq) AS unread
FROM inbox_group g
JOIN inbox_item i ON i.id = g.latest_event_id
LEFT JOIN issue iss ON iss.id = g.source_id AND g.source_kind = 'issue'
WHERE g.workspace_id = @workspace_id
  AND g.recipient_id = @recipient_id
  AND g.archived_at IS NULL
  AND (g.snoozed_until IS NULL OR g.snoozed_until <= @now)
  AND (@after_surfaced_at::timestamptz IS NULL
       OR (g.surfaced_at, g.id) < (@after_surfaced_at::timestamptz, @after_id::uuid))
ORDER BY g.surfaced_at DESC, g.id DESC
LIMIT @page_size;

-- name: ListArchivedInboxGroupsForRecipient :many
-- The archived sub-view. Mutually exclusive with the list above by construction
-- rather than by the correlated NOT EXISTS v1 needs: a group is archived or it
-- is not, and it appears in exactly one of the two.
SELECT g.*,
       i.type          AS event_type,
       i.severity      AS event_severity,
       i.title         AS event_title,
       i.body          AS event_body,
       i.actor_type    AS event_actor_type,
       i.actor_id      AS event_actor_id,
       i.details       AS event_details,
       i.issue_id      AS event_issue_id,
       i.target_kind   AS event_target_kind,
       i.target_id     AS event_target_id,
       i.created_at    AS event_created_at,
       iss.status      AS issue_status,
       (g.manual_unread OR g.read_through_seq < g.latest_seq) AS unread
FROM inbox_group g
JOIN inbox_item i ON i.id = g.latest_event_id
LEFT JOIN issue iss ON iss.id = g.source_id AND g.source_kind = 'issue'
WHERE g.workspace_id = @workspace_id
  AND g.recipient_id = @recipient_id
  AND g.archived_at IS NOT NULL
  AND (@after_surfaced_at::timestamptz IS NULL
       OR (g.surfaced_at, g.id) < (@after_surfaced_at::timestamptz, @after_id::uuid))
ORDER BY g.surfaced_at DESC, g.id DESC
LIMIT @page_size;

-- name: CountUnreadInboxGroups :one
-- One number per workspace, from one row per group. The v1 count had to fold
-- events into groups in SQL on every call and still disagreed with what the
-- client rendered.
SELECT COUNT(*) FROM inbox_group
WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id
  AND archived_at IS NULL
  AND latest_event_id IS NOT NULL
  AND (manual_unread OR read_through_seq < latest_seq);

-- name: CountUnreadInboxGroupsByWorkspace :many
-- Cross-workspace summary for the workspace switcher.
--
-- The member join is load-bearing, exactly as in v1: without it a workspace the
-- user has left keeps contributing to their switcher badge forever, and there
-- is no surface on which they could ever clear it.
SELECT g.workspace_id, COUNT(*) AS count
FROM inbox_group g
JOIN member m ON m.user_id = g.recipient_id AND m.workspace_id = g.workspace_id
WHERE g.recipient_id = @recipient_id
  AND g.archived_at IS NULL
  AND g.latest_event_id IS NOT NULL
  AND (g.manual_unread OR g.read_through_seq < g.latest_seq)
GROUP BY g.workspace_id;

-- name: GetInboxGroupWithEvent :one
-- One group with its representative, for the response a write endpoint returns.
SELECT g.*,
       i.type          AS event_type,
       i.severity      AS event_severity,
       i.title         AS event_title,
       i.body          AS event_body,
       i.actor_type    AS event_actor_type,
       i.actor_id      AS event_actor_id,
       i.details       AS event_details,
       i.issue_id      AS event_issue_id,
       i.target_kind   AS event_target_kind,
       i.target_id     AS event_target_id,
       i.created_at    AS event_created_at,
       iss.status      AS issue_status,
       (g.manual_unread OR g.read_through_seq < g.latest_seq) AS unread
FROM inbox_group g
JOIN inbox_item i ON i.id = g.latest_event_id
LEFT JOIN issue iss ON iss.id = g.source_id AND g.source_kind = 'issue'
WHERE g.id = @id AND g.workspace_id = @workspace_id AND g.recipient_id = @recipient_id;

-- name: FindInboxGroupForItem :one
-- The v1 adapters' entry point: a legacy row id arrives on a legacy endpoint,
-- and this is the group that row belongs to.
SELECT g.* FROM inbox_group g
JOIN inbox_item i ON i.group_id = g.id
WHERE i.id = @item_id AND g.workspace_id = @workspace_id AND g.recipient_id = @recipient_id;

-- name: ReconcileInboxGroupFromRows :one
-- Rows win.
--
-- The one direction in which inbox_item is authoritative over inbox_group, and
-- the reason reconcile can repair anything: v1 writes touch rows directly, so
-- during a rollback window — gate off, adapters not running, old clients still
-- marking things read — the rows move and the group does not. Recomputing the
-- group FROM the rows makes that recoverable rather than permanent.
--
-- It is a no-op whenever the two already agree, which after the adapters are
-- live is always. That is what makes it safe to run on a schedule.
--
-- The read cursor is derived the same way the lazy migration derives it: from
-- the representative row's own `read`, because v1's invariant is that at most
-- the representative is unread. manual_unread is cleared, since a v1 write is
-- the user acting through the only surface they had.
WITH survivor AS (
    SELECT event_seq, id, created_at, read
    FROM inbox_item
    WHERE group_id = @id AND dismissed_at IS NULL
    ORDER BY event_seq DESC
    LIMIT 1
),
state AS (
    SELECT
        (SELECT COUNT(*) FROM inbox_item WHERE inbox_item.group_id = @id) AS total,
        (SELECT COUNT(*) FROM inbox_item WHERE inbox_item.group_id = @id AND inbox_item.archived = false) AS active
)
UPDATE inbox_group g
SET latest_seq       = COALESCE(
        (SELECT event_seq FROM survivor),
        (SELECT COALESCE(MIN(inbox_item.event_seq), 1) - 1 FROM inbox_item WHERE inbox_item.group_id = @id)),
    latest_event_id  = (SELECT id FROM survivor),
    latest_event_at  = COALESCE((SELECT created_at FROM survivor), g.latest_event_at),
    read_through_seq = CASE
        WHEN (SELECT event_seq FROM survivor) IS NULL THEN g.read_through_seq
        WHEN (SELECT read FROM survivor) THEN (SELECT event_seq FROM survivor)
        ELSE (SELECT event_seq FROM survivor) - 1
    END,
    manual_unread    = false,
    archived_at      = CASE
        WHEN (SELECT total FROM state) = 0 THEN g.archived_at
        WHEN (SELECT active FROM state) = 0 THEN COALESCE(g.archived_at, @now)
        ELSE NULL
    END,
    state_version    = g.state_version + 1,
    updated_at       = @now
WHERE g.id = @id
RETURNING g.*;

-- name: ListInboxGroupsWithRowDrift :many
-- The groups reconcile actually has to touch.
--
-- Bounded and index-driven rather than "every group": a full pass that
-- recomputed every group would rewrite state_version for all of them and make
-- every connected client discard its cache. The predicate is the mirror
-- invariant read backwards — find the groups where at least one row disagrees
-- with what the group says it should be.
SELECT DISTINCT g.id, g.workspace_id, g.recipient_id
FROM inbox_group g
JOIN inbox_item i ON i.group_id = g.id
WHERE (@workspace_id::uuid IS NULL OR g.workspace_id = @workspace_id::uuid)
  AND (
        i.archived IS DISTINCT FROM ((g.archived_at IS NOT NULL) OR i.dismissed_at IS NOT NULL)
     OR i.read IS DISTINCT FROM (CASE
            WHEN (g.manual_unread OR g.read_through_seq < g.latest_seq)
                 AND i.event_seq = g.latest_seq THEN false
            ELSE true END)
      )
ORDER BY g.id
LIMIT @page_size;

-- name: ListRecipientsWithUnclaimedInboxItems :many
-- Who still has history to migrate. Drives the optional background pre-warm;
-- the lazy path does not need it.
SELECT DISTINCT inbox_item.recipient_id
FROM inbox_item
JOIN member ON member.user_id = inbox_item.recipient_id
           AND member.workspace_id = inbox_item.workspace_id
WHERE inbox_item.recipient_type = 'member' AND inbox_item.group_id IS NULL
ORDER BY inbox_item.recipient_id
LIMIT @page_size;

-- name: DeleteInboxGroupsForIssue :execrows
-- Lifecycle: an issue is deleted, so every group about it goes with it. No FK,
-- no cascade — the repo forbids both, so deletion is explicit here and covered
-- by the workspace/issue teardown tests.
DELETE FROM inbox_group WHERE workspace_id = @workspace_id AND source_kind = 'issue' AND source_id = @source_id;

-- name: DeleteInboxGroupsForMember :execrows
-- Lifecycle: a member left the workspace. Their groups are per-person state and
-- have no meaning once the person is gone.
DELETE FROM inbox_group WHERE workspace_id = @workspace_id AND recipient_id = @recipient_id;

-- name: DeleteOrphanInboxGroups :execrows
-- Lifecycle: groups whose every event has been deleted out from under them.
-- Without FKs nothing else removes these, and an empty group renders as
-- nothing while still counting toward a page of results.
DELETE FROM inbox_group g
WHERE g.workspace_id = @workspace_id
  AND NOT EXISTS (SELECT 1 FROM inbox_item i WHERE i.group_id = g.id);
