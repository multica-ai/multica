-- name: ListInboxItems :many
-- The issue projections feed the inbox filters. Keep this column list
-- identical to ListArchivedInboxItems: the handler converts one row type into
-- the other.
SELECT i.*,
       iss.number AS issue_number,
       iss.status AS issue_status,
       iss.priority AS issue_priority,
       iss.project_id AS issue_project_id,
       iss.assignee_type AS issue_assignee_type,
       iss.assignee_id AS issue_assignee_id,
       iss.due_date AS issue_due_date,
       ARRAY(SELECT il.label_id::text FROM issue_to_label il
             WHERE iss.id IS NOT NULL AND il.issue_id = iss.id
             ORDER BY il.label_id)::text[] AS issue_label_ids
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3 AND i.archived = false
  -- MUL-9333: a snoozed issue stays out of the inbox until its time. A row
  -- whose time has passed is listed again and keeps snoozed_until, which is
  -- how the client tells an issue that came back on its own.
  AND (i.snoozed_until IS NULL OR i.snoozed_until <= now())
  AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR NOT (COALESCE(iss.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])))
-- Newest first by when the row arrived or came back (returned_at), the order
-- idx_inbox_active_shown keeps. The
-- inbox keeps only its newest rows (inbox.ActiveLimit); inbox.Trimmer archives
-- the rest, and the limit bounds the response until it has.
ORDER BY GREATEST(i.created_at, i.returned_at) DESC, i.id DESC
LIMIT sqlc.arg('row_limit');

-- name: ListArchivedInboxItems :many
-- Archived counterpart of ListInboxItems, backing the inbox's "Archived"
-- sub-view (MUL-3736).
--
-- An issue whose group still has an active row is excluded: archiving is
-- issue-level, so a NEW notification on an already-archived issue leaves the
-- old archived rows in place alongside the fresh active one. The issue belongs
-- in the main inbox at that point, and the two lists must stay mutually
-- exclusive per issue group — otherwise the same issue renders in both. The
-- exclusion lives here rather than in the client so neither list depends on
-- the other's cache being loaded. Items without an issue_id group on their own
-- id and can never have an active sibling, hence the IS NULL short-circuit.
--
-- LIMIT applies to ISSUE GROUPS, not raw notification rows. Applying it after
-- the final SELECT lets one noisy issue consume all 200 rows and hide other
-- archived issues, which makes both the visible list and its count too small.
-- The response stays bounded at two rows per group: the newest row the UI
-- renders plus, when different, the newest row carrying a comment anchor. The
-- client already merges those two rows while deduplicating, preserving direct
-- comment landing without sending every historical notification in the group.
-- Keep the materialized working set narrow: full row data is joined only for
-- the final selected ids, not copied for every archived notification scanned.
WITH eligible_archived AS MATERIALIZED (
    SELECT i.id,
           COALESCE(i.issue_id, i.id) AS group_id,
           i.created_at,
           i.details
    FROM inbox_item i
    WHERE i.workspace_id = $1
      AND i.recipient_type = $2
      AND i.recipient_id = $3
      AND i.archived = true
      -- MUL-8472: private projects the caller cannot see.
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM issue hidden_issue
          WHERE hidden_issue.id = i.issue_id
            AND COALESCE(hidden_issue.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])
      ))
      AND (i.issue_id IS NULL OR NOT EXISTS (
          SELECT 1
          FROM inbox_item active
          WHERE active.workspace_id = i.workspace_id
            AND active.recipient_type = i.recipient_type
            AND active.recipient_id = i.recipient_id
            AND active.issue_id = i.issue_id
            AND active.archived = false
      ))
), newest_groups AS (
    SELECT DISTINCT ON (group_id)
           group_id,
           id AS newest_id,
           created_at AS newest_created_at
    FROM eligible_archived
    ORDER BY group_id, created_at DESC, id DESC
), limited_groups AS (
    SELECT group_id, newest_id
    FROM newest_groups
    ORDER BY newest_created_at DESC, newest_id DESC
    LIMIT 200
), comment_anchors AS (
    SELECT DISTINCT ON (archived.group_id)
           archived.group_id,
           archived.id
    FROM eligible_archived archived
    JOIN limited_groups selected USING (group_id)
    WHERE NULLIF(archived.details->>'comment_id', '') IS NOT NULL
    ORDER BY archived.group_id, archived.created_at DESC, archived.id DESC
), selected_ids AS (
    SELECT newest_id AS id FROM limited_groups
    UNION
    SELECT id FROM comment_anchors
)
SELECT i.*,
       iss.number AS issue_number,
       iss.status AS issue_status,
       iss.priority AS issue_priority,
       iss.project_id AS issue_project_id,
       iss.assignee_type AS issue_assignee_type,
       iss.assignee_id AS issue_assignee_id,
       iss.due_date AS issue_due_date,
       ARRAY(SELECT il.label_id::text FROM issue_to_label il
             WHERE iss.id IS NOT NULL AND il.issue_id = iss.id
             ORDER BY il.label_id)::text[] AS issue_label_ids
FROM inbox_item i
JOIN selected_ids selected ON selected.id = i.id
LEFT JOIN issue iss ON iss.id = i.issue_id
ORDER BY i.created_at DESC, i.id DESC;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = $1 AND workspace_id = $2;

-- name: CreateInboxItem :one
-- A missing recipient is a quiet skip (pgx.ErrNoRows), not a delivery failure.
-- Check the insertion snapshot without adding recipient row locks.
--
-- MUL-9333: a row written on an issue the recipient has snoozed joins the
-- snooze, so it stays out of the inbox with the rest of the issue.
-- inbox.Deliver ends the snooze instead when the row is one that
-- wakes it. A joining row is the issue's newest, so it carries the issue's
-- return rank (returned_at, the snooze end); inbox.Deliver clears it from the
-- row that held it (HandInboxReturnToRow).
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, id,
    snoozed_until, snoozed_at, returned_at
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()),
       snoozed.snoozed_until, snoozed.snoozed_at, snoozed.snoozed_until
FROM (SELECT 1) AS one
LEFT JOIN LATERAL (
    SELECT s.snoozed_until, s.snoozed_at
    FROM inbox_item s
    WHERE s.workspace_id = $1 AND s.recipient_type = $2 AND s.recipient_id = $3
      AND s.issue_id = $6 AND s.archived = false AND s.snoozed_until > now()
    ORDER BY s.snoozed_until DESC
    LIMIT 1
) AS snoozed ON true
WHERE (($2::text = 'member' AND EXISTS (
    SELECT 1 FROM member WHERE workspace_id = $1 AND user_id = $3
)) OR ($2::text = 'agent' AND EXISTS (
    SELECT 1 FROM agent WHERE workspace_id = $1 AND id = $3
)))
-- MUL-8472: a member outside a private project gets no notification about its
-- issues, whatever their workspace role. Only project members do.
AND NOT ($2::text = 'member' AND EXISTS (
    SELECT 1 FROM issue private_issue
    JOIN project private_project ON private_project.id = private_issue.project_id
    WHERE private_issue.id = $6
      AND private_project.visibility = 'private'
      AND NOT EXISTS (
          SELECT 1 FROM project_member pm
          WHERE pm.project_id = private_project.id AND pm.user_id = $3
      )
))
RETURNING *;

-- name: MarkInboxRead :one
-- Opening a row in the inbox opens its issue, so it reads every active unread
-- row on that issue, not just the one shown (MUL-8618). The inbox renders one
-- row per issue carrying its newest item; reading only that item left older
-- unread siblings (an assignment under a later comment) unread for good, out of
-- sight. This is the same rule as MarkInboxReadByIssue, which reads an issue
-- opened outside the inbox. The item itself is read whether or not it is
-- archived; its siblings only while active, as in MarkInboxReadByIssue, which
-- keeps the probe on the partial index idx_inbox_active_by_issue. Items without
-- an issue are their own group. siblings_read is how many other rows it read.
WITH target AS (
    SELECT t.workspace_id, t.recipient_type, t.recipient_id, t.issue_id
    FROM inbox_item t
    WHERE t.id = $1 AND t.issue_id IS NOT NULL
), siblings AS (
    UPDATE inbox_item s SET read = true
    FROM target
    WHERE s.workspace_id = target.workspace_id
      AND s.recipient_type = target.recipient_type
      AND s.recipient_id = target.recipient_id
      AND s.issue_id = target.issue_id
      AND s.archived = false AND s.read = false
      AND s.id <> $1
    RETURNING s.id
)
UPDATE inbox_item AS item SET read = true
WHERE item.id = $1
RETURNING sqlc.embed(item), (SELECT count(*) FROM siblings)::bigint AS siblings_read;

-- name: MarkInboxUnread :one
-- Item-level, unlike MarkInboxRead: the inbox renders one row per issue
-- carrying that group's NEWEST item, and the group's read state is that item's
-- read state, so flipping the newest item is enough to make the row unread.
-- Flipping the whole group unread would resurrect older siblings the user
-- already dealt with and inflate CountUnreadInbox (which counts raw rows),
-- while changing nothing the UI shows.
UPDATE inbox_item SET read = false
WHERE id = $1
RETURNING *;

-- name: ArchiveInboxItem :one
-- Archiving ends a snooze (MUL-9333), on every archive path below: a row
-- restored later must not come back still hidden.
UPDATE inbox_item SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE id = $1
RETURNING *;

-- name: DeleteInboxGroup :execrows
-- A visible inbox row groups notifications on an issue. Remove that group
-- only in the selected view, so older siblings cannot bring the row back and
-- deleting an active row does not erase its archived history.
WITH target AS (
    SELECT selected.* FROM inbox_item selected
    WHERE selected.id = $1 AND selected.workspace_id = $2
      AND selected.recipient_type = 'member' AND selected.recipient_id = $3
)
DELETE FROM inbox_item item USING target
WHERE item.workspace_id = target.workspace_id
  AND item.recipient_type = target.recipient_type
  AND item.recipient_id = target.recipient_id
  AND item.archived = target.archived
  AND (item.id = target.id OR item.issue_id = target.issue_id);

-- name: ArchiveInboxByIssue :execrows
UPDATE inbox_item SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: UnarchiveInboxItem :one
-- Deliberately does not touch `read`: unarchiving restores an item to the main
-- inbox in the exact read/unread state it was archived in, so restoring an
-- unread item legitimately raises the unread badge again (MUL-3736).
-- returned_at ranks the row as just back for the inbox limit, so a restored
-- old notification is not trimmed straight back into the archive. Only the
-- restored row: its issue's older rows, restored by UnarchiveInboxByIssue,
-- keep their rank so a busy issue does not outrank the whole inbox.
UPDATE inbox_item SET archived = false, returned_at = now()
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxByIssue :execrows
-- Issue-level restore, mirroring ArchiveInboxByIssue: archiving one item
-- archives every sibling for the same issue, so unarchiving must bring the
-- whole group back. Leaves `read` untouched for the same reason as above.
UPDATE inbox_item SET archived = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = true;

-- name: ArchiveInboxByIssueAndType :many
UPDATE inbox_item SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE workspace_id = $1 AND issue_id = $2 AND type = $3 AND archived = false
RETURNING recipient_type, recipient_id;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item i
WHERE i.workspace_id = sqlc.arg('workspace_id')
  AND i.recipient_type = sqlc.arg('recipient_type')
  AND i.recipient_id = sqlc.arg('recipient_id')
  AND i.read = false
  AND i.archived = false
  AND (i.snoozed_until IS NULL OR i.snoozed_until <= now())
  -- MUL-8472: private projects the caller cannot see.
  AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.issue_id IS NULL OR NOT EXISTS (
      SELECT 1 FROM issue hidden_issue
      WHERE hidden_issue.id = i.issue_id
        AND COALESCE(hidden_issue.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])
  ));

-- name: CountUnreadInboxByWorkspace :many
-- Per-workspace unread inbox counts for a recipient member, matching the
-- inbox UI's deduplicated view: notifications are grouped per issue
-- (Linear-style, one row per issue) and an issue counts as unread only when
-- its NEWEST non-archived item is unread. Opening an issue reads the whole
-- group now (MUL-8618), but rows read before that read only the newest item,
-- so counting raw unread rows would keep those older siblings alive and light
-- the switcher dot for a workspace whose inbox the user sees as empty
-- (MUL-3695). Items without an issue group on their own id. The
-- member join keeps counts scoped to workspaces the user still belongs to,
-- so a stale item left behind in a workspace the user has since left cannot
-- light the dot.
SELECT newest.workspace_id, count(*) AS count
FROM (
    SELECT DISTINCT ON (i.workspace_id, COALESCE(i.issue_id, i.id))
        i.workspace_id, i.read
    FROM inbox_item i
    JOIN member m ON m.workspace_id = i.workspace_id AND m.user_id = i.recipient_id
    WHERE i.recipient_type = 'member'
      AND i.recipient_id = $1
      AND i.archived = false
      -- MUL-9333: a snoozed issue does not count until it comes back.
      AND (i.snoozed_until IS NULL OR i.snoozed_until <= now())
      -- MUL-8472: issues in private projects the recipient is not a member
      -- of do not count.
      AND NOT EXISTS (
          SELECT 1 FROM issue private_issue
          JOIN project private_project ON private_project.id = private_issue.project_id
          WHERE private_issue.id = i.issue_id
            AND private_project.visibility = 'private'
            AND NOT EXISTS (
                SELECT 1 FROM project_member pm
                WHERE pm.project_id = private_project.id AND pm.user_id = i.recipient_id
            )
      )
    ORDER BY i.workspace_id, COALESCE(i.issue_id, i.id), i.created_at DESC
) newest
WHERE newest.read = false
GROUP BY newest.workspace_id;

-- name: MarkAllInboxRead :execrows
-- The batch actions act on what the inbox lists, so they skip snoozed issues
-- (MUL-9333).
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false
  AND (snoozed_until IS NULL OR snoozed_until <= now());

-- name: MarkInboxReadByIssue :execrows
-- Reading an issue outside the inbox (its page, a side peek, the Unread view)
-- reads its notifications too, so the inbox does not ask about the same issue
-- again (MUL-8588). Every unread row in the issue's group, not just the newest,
-- whatever its severity: opening the issue shows all of it, so an assignment,
-- review request or mention on it counts as seen (MUL-8618; this replaces the
-- MUL-8596 rule that kept those unread). Opening the row in the inbox does the
-- same through MarkInboxRead. Archived rows are left as they are, as in
-- MarkAllInboxRead, which also keeps the lookup on the partial index
-- idx_inbox_active_by_issue.
-- A snoozed issue is left unread: viewing it elsewhere does not end the
-- snooze, and it comes back as something to look at (MUL-9333).
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND issue_id = $3 AND archived = false AND read = false
  AND (snoozed_until IS NULL OR snoozed_until <= now());

-- name: ArchiveAllInbox :execrows
-- Every archive clears returned_at: a row restored later earns its rank anew
-- (UnarchiveInboxItem), so old return times never outrank the inbox again.
UPDATE inbox_item SET archived = true, returned_at = NULL
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false
  AND (snoozed_until IS NULL OR snoozed_until <= now());

-- name: ArchiveAllReadInbox :execrows
-- "Read" is the state of the one issue row the inbox renders: the newest
-- active notification in that issue group. Archive every row in groups whose
-- newest row is read, and leave an unread group wholly untouched. Updating raw
-- read rows instead makes an older unread sibling reappear after the newest
-- row is archived (and can archive an older read sibling under an unread row).
WITH newest_groups AS (
    SELECT DISTINCT ON (COALESCE(i.issue_id, i.id))
           COALESCE(i.issue_id, i.id) AS group_id,
           i.read
    FROM inbox_item i
    WHERE i.workspace_id = $1
      AND i.recipient_type = 'member'
      AND i.recipient_id = $2
      AND i.archived = false
      AND (i.snoozed_until IS NULL OR i.snoozed_until <= now())
    ORDER BY COALESCE(i.issue_id, i.id), i.created_at DESC, i.id DESC
), read_groups AS (
    SELECT group_id
    FROM newest_groups
    WHERE read = true
)
UPDATE inbox_item i SET archived = true, returned_at = NULL
FROM read_groups selected
WHERE i.workspace_id = $1
  AND i.recipient_type = 'member'
  AND i.recipient_id = $2
  AND i.archived = false
  AND COALESCE(i.issue_id, i.id) = selected.group_id;

-- name: ArchiveCompletedInbox :execrows
-- Snoozed issues included: a done issue leaves the inbox either way.
UPDATE inbox_item i SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.issue_id IN (
    SELECT id FROM issue
    WHERE workspace_id = $1
      AND status = ANY(sqlc.arg('terminal_status_keys')::text[])
  );

-- name: SnoozeInboxGroup :many
-- Snooze is issue-level, like archive (MUL-9333): every active row the
-- recipient has on the item's issue carries the time it comes back. Changing
-- the time of a running snooze keeps when it started, so the count of what
-- arrived meanwhile carries on. The newest row turns unread, so the issue
-- comes back as something to look at.
WITH target AS (
    SELECT t.workspace_id, t.recipient_type, t.recipient_id, t.issue_id
    FROM inbox_item t
    WHERE t.id = sqlc.arg('id') AND t.issue_id IS NOT NULL AND t.archived = false
), newest AS (
    SELECT n.id
    FROM inbox_item n
    JOIN target ON n.workspace_id = target.workspace_id
        AND n.recipient_type = target.recipient_type
        AND n.recipient_id = target.recipient_id
        AND n.issue_id = target.issue_id
    WHERE n.archived = false
    ORDER BY n.created_at DESC, n.id DESC
    LIMIT 1
)
UPDATE inbox_item item
SET snoozed_until = sqlc.arg('snoozed_until')::timestamptz,
    snoozed_at = CASE WHEN item.snoozed_until > now() AND item.snoozed_at IS NOT NULL THEN item.snoozed_at ELSE now() END,
    read = CASE WHEN item.id = (SELECT id FROM newest) THEN false ELSE item.read END,
    -- The issue comes back on top when the snooze ends: the newest row ranks
    -- by that time for the inbox limit, the others by when they arrived.
    returned_at = CASE WHEN item.id = (SELECT id FROM newest) THEN sqlc.arg('snoozed_until')::timestamptz END
FROM target
WHERE item.workspace_id = target.workspace_id
  AND item.recipient_type = target.recipient_type
  AND item.recipient_id = target.recipient_id
  AND item.issue_id = target.issue_id
  AND item.archived = false
RETURNING item.*;

-- name: UnsnoozeInboxGroup :many
-- Ends a snooze on one issue: back in the inbox now. Also clears the time a
-- snooze that already ran out left on the rows. The issue's newest row ranks
-- as back now for the inbox limit, or when the snooze ran out if it already
-- had; the others rank by when they arrived.
WITH newest AS (
    SELECT n.id FROM inbox_item n
    WHERE n.workspace_id = $1 AND n.recipient_type = $2 AND n.recipient_id = $3 AND n.issue_id = $4
      AND n.archived = false
    ORDER BY n.created_at DESC, n.id DESC
    LIMIT 1
)
UPDATE inbox_item AS item SET snoozed_until = NULL, snoozed_at = NULL,
    returned_at = CASE WHEN item.id = (SELECT id FROM newest) THEN LEAST(item.snoozed_until, now()) END
WHERE item.workspace_id = $1 AND item.recipient_type = $2 AND item.recipient_id = $3 AND item.issue_id = $4
  AND item.archived = false AND item.snoozed_until IS NOT NULL
RETURNING item.*;

-- name: ListSnoozedInbox :many
-- The recipient's snoozed issues, soonest back first (MUL-9333). `updates` is
-- how many rows arrived while snoozed; `item_id` is the issue's newest row,
-- the one the snooze endpoints take.
SELECT g.issue_id::uuid AS issue_id,
       newest.id AS item_id,
       g.snoozed_until::timestamptz AS snoozed_until,
       g.snoozed_at::timestamptz AS snoozed_at,
       g.updates::bigint AS updates
FROM (
    SELECT i.issue_id,
           max(i.snoozed_until) AS snoozed_until,
           min(i.snoozed_at) AS snoozed_at,
           count(*) FILTER (WHERE i.snoozed_at IS NOT NULL AND i.created_at > i.snoozed_at) AS updates
    FROM inbox_item i
    WHERE i.workspace_id = sqlc.arg('workspace_id')
      AND i.recipient_type = 'member'
      AND i.recipient_id = sqlc.arg('recipient_id')
      AND i.archived = false
      AND i.issue_id IS NOT NULL
      AND i.snoozed_until > now()
      -- MUL-8472: private projects the caller cannot see.
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR NOT EXISTS (
          SELECT 1 FROM issue hidden_issue
          WHERE hidden_issue.id = i.issue_id
            AND COALESCE(hidden_issue.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])
      ))
    GROUP BY i.issue_id
) g
JOIN LATERAL (
    SELECT n.id
    FROM inbox_item n
    WHERE n.workspace_id = sqlc.arg('workspace_id')
      AND n.recipient_type = 'member'
      AND n.recipient_id = sqlc.arg('recipient_id')
      AND n.issue_id = g.issue_id
      AND n.archived = false
    ORDER BY n.created_at DESC, n.id DESC
    LIMIT 1
) newest ON true
ORDER BY g.snoozed_until ASC, g.issue_id;

-- name: InboxOverflows :one
-- Whether the recipient's inbox shows more than `keep` rows, by the rules
-- ListInboxItems shows them: active, not snoozed, outside the projects the
-- recipient cannot see (hidden_project_ids), ranked by when they arrived or
-- came back.
SELECT count(*) > 0 FROM (
    SELECT 1 FROM inbox_item i
    LEFT JOIN issue iss ON iss.id = i.issue_id
    WHERE i.workspace_id = $1 AND i.recipient_type = $2 AND i.recipient_id = $3
      AND i.archived = false
      AND (i.snoozed_until IS NULL OR i.snoozed_until <= now())
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL
           OR NOT (COALESCE(iss.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])))
    ORDER BY GREATEST(i.created_at, i.returned_at) DESC, i.id DESC
    OFFSET sqlc.arg('keep')::int
    LIMIT 1
) beyond;

-- name: ArchiveInboxOverflow :execrows
-- Archives up to batch_size of the recipient's shown rows ranked below the
-- newest `keep`, oldest first, in ListInboxItems' order. The boundary is read
-- in the same statement, so every batch sees the inbox as it is now. Rows are
-- archived one by one: unlike the issue-level archive actions, newer rows on
-- the same issue stay. Snoozed rows, and rows in projects the recipient can
-- no longer see, neither count nor get archived: a snooze is never ended
-- behind the user's back, and hidden rows never push out visible ones. SKIP LOCKED keeps a trim from waiting on, or
-- deadlocking with, the user's own inbox writes; InboxOverflows tells the
-- caller whether skipped rows remain.
-- MATERIALIZED: the boundary is read once per statement, not once per row.
WITH boundary AS MATERIALIZED (
    SELECT GREATEST(b.created_at, b.returned_at) AS shown_at, b.id
    FROM inbox_item b
    LEFT JOIN issue b_iss ON b_iss.id = b.issue_id
    WHERE b.workspace_id = sqlc.arg('workspace_id') AND b.recipient_type = sqlc.arg('recipient_type')
      AND b.recipient_id = sqlc.arg('recipient_id')
      AND b.archived = false
      AND (b.snoozed_until IS NULL OR b.snoozed_until <= now())
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL
           OR NOT (COALESCE(b_iss.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])))
    ORDER BY GREATEST(b.created_at, b.returned_at) DESC, b.id DESC
    OFFSET sqlc.arg('keep')::int - 1
    LIMIT 1
), overflow AS (
    SELECT o.id FROM inbox_item o
    CROSS JOIN boundary
    LEFT JOIN issue o_iss ON o_iss.id = o.issue_id
    WHERE o.workspace_id = sqlc.arg('workspace_id') AND o.recipient_type = sqlc.arg('recipient_type')
      AND o.recipient_id = sqlc.arg('recipient_id')
      AND o.archived = false
      AND (o.snoozed_until IS NULL OR o.snoozed_until <= now())
      AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL
           OR NOT (COALESCE(o_iss.project_id, '00000000-0000-0000-0000-000000000000'::uuid) = ANY(sqlc.narg('hidden_project_ids')::uuid[])))
      AND (GREATEST(o.created_at, o.returned_at), o.id) < (boundary.shown_at, boundary.id)
      -- An issue back from a snooze whose newest row does not carry the
      -- return yet (written by an older server, or not normalized) ranks too
      -- low here; never archive that row on its stale rank.
      AND NOT (o.issue_id IS NOT NULL AND o.snoozed_until IS NOT NULL
               AND (o.returned_at IS NULL OR o.returned_at < o.snoozed_until)
               AND NOT EXISTS (
                   SELECT 1 FROM inbox_item newer
                   WHERE newer.workspace_id = o.workspace_id AND newer.recipient_type = o.recipient_type
                     AND newer.recipient_id = o.recipient_id AND newer.issue_id = o.issue_id
                     AND newer.archived = false
                     AND (newer.created_at, newer.id) > (o.created_at, o.id)
               ))
    ORDER BY GREATEST(o.created_at, o.returned_at), o.id
    LIMIT sqlc.arg('batch_size')::int
    FOR UPDATE OF o SKIP LOCKED
)
-- ANY(ARRAY(...)) keeps the update on primary-key lookups; a join against the
-- CTE lets the planner pick a scan of the whole table.
UPDATE inbox_item AS item SET archived = true, snoozed_until = NULL, snoozed_at = NULL, returned_at = NULL
WHERE item.id = ANY (ARRAY(SELECT overflow.id FROM overflow));

-- name: NormalizeInboxReturns :execrows
-- Leaves at most one row per issue ranked by its return (returned_at): the
-- issue's newest active row, carrying the latest return any of its rows had,
-- or its snooze time. Writes keep this true; this repairs what they could not,
-- such as rows an older server wrote during a rollout. Fixes up to batch_size
-- rows, skipping rows another write holds, so it never waits on a lock; the
-- caller runs it again while it returns a full batch.
-- MATERIALIZED: each step runs once; inlined, a planner without statistics
-- re-ran them for every row.
WITH marked AS MATERIALIZED (
    SELECT g.issue_id, max(g.returned_at) AS returned_at
    FROM inbox_item g
    WHERE g.workspace_id = sqlc.arg('workspace_id') AND g.recipient_type = sqlc.arg('recipient_type')
      AND g.recipient_id = sqlc.arg('recipient_id')
      AND g.archived = false AND g.issue_id IS NOT NULL
      AND (g.returned_at IS NOT NULL OR g.snoozed_until IS NOT NULL)
    GROUP BY g.issue_id
), wanted AS MATERIALIZED (
    SELECT DISTINCT ON (r.issue_id) r.issue_id, r.id,
           GREATEST(marked.returned_at, r.snoozed_until) AS returned_at
    FROM inbox_item r
    JOIN marked ON marked.issue_id = r.issue_id
    WHERE r.workspace_id = sqlc.arg('workspace_id') AND r.recipient_type = sqlc.arg('recipient_type')
      AND r.recipient_id = sqlc.arg('recipient_id')
      AND r.archived = false
    ORDER BY r.issue_id, r.created_at DESC, r.id DESC
), stale AS (
    SELECT item.id, CASE WHEN item.id = wanted.id THEN wanted.returned_at END AS returned_at
    FROM inbox_item item
    JOIN wanted ON wanted.issue_id = item.issue_id
    WHERE item.workspace_id = sqlc.arg('workspace_id') AND item.recipient_type = sqlc.arg('recipient_type')
      AND item.recipient_id = sqlc.arg('recipient_id')
      AND item.archived = false
      AND item.returned_at IS DISTINCT FROM (CASE WHEN item.id = wanted.id THEN wanted.returned_at END)
    ORDER BY item.issue_id, item.id
    LIMIT sqlc.arg('batch_size')::int
    FOR UPDATE OF item SKIP LOCKED
)
UPDATE inbox_item AS fixed
SET returned_at = (SELECT stale.returned_at FROM stale WHERE stale.id = fixed.id)
WHERE fixed.id = ANY (ARRAY(SELECT stale.id FROM stale));

-- name: HandInboxReturnToRow :exec
-- Clears the issue's return rank from the active rows older than keep_id, the
-- row that now carries it (see CreateInboxItem). Only older rows: two joining
-- rows handing over concurrently, in either order, leave the rank on the newer.
UPDATE inbox_item AS item SET returned_at = NULL
FROM inbox_item AS kept
WHERE kept.id = sqlc.arg('keep_id')
  AND item.workspace_id = $1 AND item.recipient_type = $2 AND item.recipient_id = $3 AND item.issue_id = $4
  AND item.archived = false AND item.returned_at IS NOT NULL
  AND (item.created_at, item.id) < (kept.created_at, kept.id);

-- name: InboxReturnsSettled :one
-- Whether NormalizeInboxReturns has nothing left to fix for the recipient,
-- rows another write holds included. The inbox limit archives only then, so
-- an unsettled duplicate rank never widens the set of rows it keeps.
WITH marked AS MATERIALIZED (
    SELECT g.issue_id, max(g.returned_at) AS returned_at
    FROM inbox_item g
    WHERE g.workspace_id = $1 AND g.recipient_type = $2 AND g.recipient_id = $3
      AND g.archived = false AND g.issue_id IS NOT NULL
      AND (g.returned_at IS NOT NULL OR g.snoozed_until IS NOT NULL)
    GROUP BY g.issue_id
), wanted AS MATERIALIZED (
    SELECT DISTINCT ON (r.issue_id) r.issue_id, r.id,
           GREATEST(marked.returned_at, r.snoozed_until) AS returned_at
    FROM inbox_item r
    JOIN marked ON marked.issue_id = r.issue_id
    WHERE r.workspace_id = $1 AND r.recipient_type = $2 AND r.recipient_id = $3
      AND r.archived = false
    ORDER BY r.issue_id, r.created_at DESC, r.id DESC
)
SELECT count(*) = 0 FROM (
    SELECT 1 FROM inbox_item item
    JOIN wanted ON wanted.issue_id = item.issue_id
    WHERE item.workspace_id = $1 AND item.recipient_type = $2 AND item.recipient_id = $3
      AND item.archived = false
      AND item.returned_at IS DISTINCT FROM (CASE WHEN item.id = wanted.id THEN wanted.returned_at END)
    LIMIT 1
) unsettled;

-- name: BackfillInboxSnoozeReturns :execrows
-- Gives up to batch_size snoozed issues their return rank where a writer that
-- predates returned_at left it out: the issue's newest active row ranks by
-- when the snooze ends. Snoozes set before the column existed, and by an
-- older server during the rollout. Skips rows another write holds and
-- touches only rows still without a rank, so it can run again at any time.
-- workspace_id narrows it for tests; production passes NULL.
WITH candidates AS (
    SELECT s.id
    FROM inbox_item s
    WHERE (sqlc.narg('workspace_id')::uuid IS NULL OR s.workspace_id = sqlc.narg('workspace_id')::uuid)
      AND s.archived = false AND s.issue_id IS NOT NULL
      AND s.snoozed_until IS NOT NULL AND s.returned_at IS NULL
      AND NOT EXISTS (
          SELECT 1 FROM inbox_item newer
          WHERE newer.workspace_id = s.workspace_id AND newer.recipient_type = s.recipient_type
            AND newer.recipient_id = s.recipient_id AND newer.issue_id = s.issue_id
            AND newer.archived = false
            AND (newer.created_at, newer.id) > (s.created_at, s.id)
      )
    LIMIT sqlc.arg('batch_size')::int
    FOR UPDATE OF s SKIP LOCKED
)
UPDATE inbox_item AS item SET returned_at = item.snoozed_until
WHERE item.id = ANY (ARRAY(SELECT candidates.id FROM candidates));
