-- name: ListPinnedItems :many
-- A pin is only navigation state. If the user later loses access to the
-- item's Private Space, the stale pin must disappear instead of becoming a
-- back door to the item's identity.
SELECT p.*
FROM pinned_item p
LEFT JOIN issue i
  ON p.item_type = 'issue' AND i.id = p.item_id
LEFT JOIN project pr
  ON p.item_type = 'project' AND pr.id = p.item_id
JOIN workspace_space s
  ON s.id = COALESCE(i.space_id, pr.space_id)
JOIN member wm
  ON wm.workspace_id = p.workspace_id AND wm.user_id = p.user_id
LEFT JOIN workspace_space_member sm
  ON sm.space_id = s.id AND sm.user_id = p.user_id
WHERE p.workspace_id = $1
  AND p.user_id = $2
  AND (s.visibility = 'open' OR sm.user_id IS NOT NULL OR wm.role IN ('owner', 'admin'))
ORDER BY p.position ASC, p.created_at ASC;

-- name: CreatePinnedItem :one
INSERT INTO pinned_item (workspace_id, user_id, item_type, item_id, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeletePinnedItem :exec
DELETE FROM pinned_item
WHERE workspace_id = $1 AND user_id = $2 AND item_type = $3 AND item_id = $4;

-- name: UpdatePinnedItemPosition :exec
UPDATE pinned_item SET position = $1
WHERE id = $2 AND workspace_id = $3 AND user_id = $4;

-- name: GetMaxPinnedItemPosition :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM pinned_item
WHERE workspace_id = $1 AND user_id = $2;

-- name: DeletePinnedItemsByItem :exec
DELETE FROM pinned_item
WHERE item_type = $1 AND item_id = $2;
