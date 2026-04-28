-- name: ListInboxFolders :many
SELECT * FROM inbox_folder
WHERE workspace_id = $1 AND user_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetInboxFolder :one
SELECT * FROM inbox_folder
WHERE id = $1 AND workspace_id = $2 AND user_id = $3;

-- name: CreateInboxFolder :one
INSERT INTO inbox_folder (workspace_id, user_id, name, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: RenameInboxFolder :one
UPDATE inbox_folder SET name = $1
WHERE id = $2 AND workspace_id = $3 AND user_id = $4
RETURNING *;

-- name: UpdateInboxFolderPosition :exec
UPDATE inbox_folder SET position = $1
WHERE id = $2 AND workspace_id = $3 AND user_id = $4;

-- name: DeleteInboxFolder :exec
DELETE FROM inbox_folder
WHERE id = $1 AND workspace_id = $2 AND user_id = $3;

-- name: GetMaxInboxFolderPosition :one
SELECT COALESCE(MAX(position), 0)::float8 AS max_position
FROM inbox_folder
WHERE workspace_id = $1 AND user_id = $2;

-- name: AddItemToFolder :exec
INSERT INTO inbox_folder_membership (folder_id, item_type, item_id)
VALUES ($1, $2, $3)
ON CONFLICT (folder_id, item_type, item_id) DO NOTHING;

-- name: RemoveItemFromFolder :exec
DELETE FROM inbox_folder_membership
WHERE folder_id = $1 AND item_type = $2 AND item_id = $3;

-- name: RemoveItemFromAllFolders :exec
-- Removes the item from every folder belonging to this user (workspace
-- already implied by folder ownership). Used when reverting an item to the
-- default inbox view.
DELETE FROM inbox_folder_membership m
USING inbox_folder f
WHERE m.folder_id = f.id
  AND f.workspace_id = $1
  AND f.user_id = $2
  AND m.item_type = $3
  AND m.item_id = $4;

-- name: ListInboxFolderMemberships :many
-- All folder memberships for the current user. Used by the frontend to know
-- which items belong to which folder without an N+1.
SELECT m.folder_id, m.item_type, m.item_id, m.added_at
FROM inbox_folder_membership m
JOIN inbox_folder f ON f.id = m.folder_id
WHERE f.workspace_id = $1 AND f.user_id = $2;

-- name: ListInboxItemsUnfiled :many
-- Inbox items that are not archived and not in any folder.
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND NOT EXISTS (
    SELECT 1 FROM inbox_folder_membership m
    WHERE m.item_type = 'notification' AND m.item_id = i.id
  )
ORDER BY i.created_at DESC;

-- name: ListInboxItemsInFolder :many
-- Inbox items in a specific folder (regardless of archived flag).
SELECT i.*,
       iss.status as issue_status
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
JOIN inbox_folder_membership m
  ON m.item_type = 'notification' AND m.item_id = i.id
JOIN inbox_folder f
  ON f.id = m.folder_id AND f.id = $1 AND f.workspace_id = $2 AND f.user_id = $3
ORDER BY i.created_at DESC;

-- name: ListChatSessionsUnfiled :many
-- Active chat sessions (creator-owned) not in any folder.
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
WHERE cs.workspace_id = $1
  AND cs.creator_id = $2
  AND cs.status = 'active'
  AND NOT EXISTS (
    SELECT 1 FROM inbox_folder_membership m
    WHERE m.item_type = 'chat_session' AND m.item_id = cs.id
  )
ORDER BY cs.updated_at DESC;

-- name: ListChatSessionsInFolder :many
-- Chat sessions in a specific folder (regardless of status).
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
JOIN inbox_folder_membership m
  ON m.item_type = 'chat_session' AND m.item_id = cs.id
JOIN inbox_folder f
  ON f.id = m.folder_id AND f.id = $1 AND f.workspace_id = $2 AND f.user_id = $3
ORDER BY cs.updated_at DESC;
