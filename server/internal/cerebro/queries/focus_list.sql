-- CEREBRO-PATCH(cerebro-focus-list): FIR-2947 — personal focus list for inbox.

-- name: ListFocusListItems :many
-- All items for a member ordered by position asc then created_at asc.
-- Includes done and snoozed items so the full list can be rendered client-side
-- (the frontend filters by done_at / snoozed_until).
SELECT id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at
FROM cerebro_focus_list_item
WHERE workspace_id = $1 AND member_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetFocusListItem :one
SELECT id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at
FROM cerebro_focus_list_item
WHERE id = $1;

-- name: CreateFocusListItem :one
INSERT INTO cerebro_focus_list_item (workspace_id, member_id, text, issue_id, position)
VALUES ($1, $2, $3, $4, COALESCE(
    (SELECT MAX(position) FROM cerebro_focus_list_item WHERE workspace_id = $1 AND member_id = $2),
    0
) + 1)
RETURNING id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at;

-- name: UpdateFocusListItem :one
UPDATE cerebro_focus_list_item
SET text       = COALESCE(sqlc.narg('text'), text),
    issue_id   = sqlc.narg('issue_id'),
    updated_at = NOW()
WHERE id = $1
RETURNING id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at;

-- name: MarkFocusListItemDone :one
UPDATE cerebro_focus_list_item
SET done_at    = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at;

-- name: SnoozeFocusListItem :one
UPDATE cerebro_focus_list_item
SET snoozed_until = $2,
    updated_at    = NOW()
WHERE id = $1
RETURNING id, workspace_id, member_id, text, issue_id, snoozed_until, done_at, position, created_at, updated_at;

-- name: DeleteFocusListItem :exec
DELETE FROM cerebro_focus_list_item WHERE id = $1;

-- name: SetFocusListItemPosition :exec
UPDATE cerebro_focus_list_item
SET position = $2,
    updated_at = NOW()
WHERE id = $1;
