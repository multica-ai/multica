-- name: ListInboxItems :many
-- The issue projections feed the inbox filters. Keep this column list
-- identical to ListArchivedInboxItems: the handler converts one row type into
-- the other.
SELECT i.*,
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
  AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR iss.project_id IS NULL OR NOT (iss.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])))
ORDER BY i.created_at DESC;

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
            AND hidden_issue.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])
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

-- name: GetInboxItem :one
SELECT * FROM inbox_item
WHERE id = $1;

-- name: GetInboxItemInWorkspace :one
SELECT * FROM inbox_item
WHERE id = $1 AND workspace_id = $2;

-- name: CreateInboxItem :one
-- A missing recipient is a quiet skip (pgx.ErrNoRows), not a delivery failure.
-- Check the insertion snapshot without adding recipient row locks.
INSERT INTO inbox_item (
    workspace_id, recipient_type, recipient_id,
    type, severity, issue_id, title, body,
    actor_type, actor_id, details, id
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
WHERE (($2::text = 'member' AND EXISTS (
    SELECT 1 FROM member WHERE workspace_id = $1 AND user_id = $3
)) OR ($2::text = 'agent' AND EXISTS (
    SELECT 1 FROM agent WHERE workspace_id = $1 AND id = $3
)))
-- MUL-8472: a member outside a private project gets no notification about its
-- issues. Owners/admins and project members do.
AND NOT ($2::text = 'member' AND EXISTS (
    SELECT 1 FROM issue private_issue
    JOIN project private_project ON private_project.id = private_issue.project_id
    WHERE private_issue.id = $6
      AND private_project.visibility = 'private'
      AND NOT EXISTS (
          SELECT 1 FROM project_member pm
          WHERE pm.project_id = private_project.id AND pm.user_id = $3
      )
      AND NOT EXISTS (
          SELECT 1 FROM member manager
          WHERE manager.workspace_id = $1 AND manager.user_id = $3
            AND manager.role IN ('owner', 'admin')
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
-- an issue are their own group.
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
)
UPDATE inbox_item AS item SET read = true
WHERE item.id = $1
RETURNING item.*;

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
UPDATE inbox_item SET archived = true
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
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = false;

-- name: UnarchiveInboxItem :one
-- Deliberately does not touch `read`: unarchiving restores an item to the main
-- inbox in the exact read/unread state it was archived in, so restoring an
-- unread item legitimately raises the unread badge again (MUL-3736).
UPDATE inbox_item SET archived = false
WHERE id = $1
RETURNING *;

-- name: UnarchiveInboxByIssue :execrows
-- Issue-level restore, mirroring ArchiveInboxByIssue: archiving one item
-- archives every sibling for the same issue, so unarchiving must bring the
-- whole group back. Leaves `read` untouched for the same reason as above.
UPDATE inbox_item SET archived = false
WHERE workspace_id = $1 AND recipient_type = $2 AND recipient_id = $3 AND issue_id = $4 AND archived = true;

-- name: ArchiveInboxByIssueAndType :many
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND issue_id = $2 AND type = $3 AND archived = false
RETURNING recipient_type, recipient_id;

-- name: CountUnreadInbox :one
SELECT count(*) FROM inbox_item i
WHERE i.workspace_id = sqlc.arg('workspace_id')
  AND i.recipient_type = sqlc.arg('recipient_type')
  AND i.recipient_id = sqlc.arg('recipient_id')
  AND i.read = false
  AND i.archived = false
  -- MUL-8472: private projects the caller cannot see.
  AND (sqlc.narg('hidden_project_ids')::uuid[] IS NULL OR i.issue_id IS NULL OR NOT EXISTS (
      SELECT 1 FROM issue hidden_issue
      WHERE hidden_issue.id = i.issue_id
        AND hidden_issue.project_id = ANY(sqlc.narg('hidden_project_ids')::uuid[])
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
      -- MUL-8472: issues in private projects the recipient is outside of
      -- (neither a project member nor a workspace owner/admin) do not count.
      AND NOT (m.role NOT IN ('owner', 'admin') AND EXISTS (
          SELECT 1 FROM issue private_issue
          JOIN project private_project ON private_project.id = private_issue.project_id
          WHERE private_issue.id = i.issue_id
            AND private_project.visibility = 'private'
            AND NOT EXISTS (
                SELECT 1 FROM project_member pm
                WHERE pm.project_id = private_project.id AND pm.user_id = i.recipient_id
            )
      ))
    ORDER BY i.workspace_id, COALESCE(i.issue_id, i.id), i.created_at DESC
) newest
WHERE newest.read = false
GROUP BY newest.workspace_id;

-- name: MarkAllInboxRead :execrows
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false AND read = false;

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
UPDATE inbox_item SET read = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2
  AND issue_id = $3 AND archived = false AND read = false;

-- name: ArchiveAllInbox :execrows
UPDATE inbox_item SET archived = true
WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND archived = false;

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
    ORDER BY COALESCE(i.issue_id, i.id), i.created_at DESC, i.id DESC
), read_groups AS (
    SELECT group_id
    FROM newest_groups
    WHERE read = true
)
UPDATE inbox_item i SET archived = true
FROM read_groups selected
WHERE i.workspace_id = $1
  AND i.recipient_type = 'member'
  AND i.recipient_id = $2
  AND i.archived = false
  AND COALESCE(i.issue_id, i.id) = selected.group_id;

-- name: ArchiveCompletedInbox :execrows
UPDATE inbox_item i SET archived = true
WHERE i.workspace_id = $1 AND i.recipient_type = 'member' AND i.recipient_id = $2 AND i.archived = false
  AND i.issue_id IN (
    SELECT id FROM issue
    WHERE workspace_id = $1
      AND status = ANY(sqlc.arg('terminal_status_keys')::text[])
  );
