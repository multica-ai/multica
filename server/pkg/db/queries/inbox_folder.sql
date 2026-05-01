-- name: ListInboxFolders :many
SELECT * FROM inbox_folder
WHERE workspace_id = $1 AND user_id = $2
ORDER BY position ASC, created_at ASC;

-- name: GetInboxFolder :one
SELECT * FROM inbox_folder
WHERE id = $1 AND workspace_id = $2 AND user_id = $3;

-- name: CreateInboxFolder :one
INSERT INTO inbox_folder (workspace_id, user_id, name, position, parent_id)
VALUES ($1, $2, $3, $4, sqlc.narg('parent_id'))
RETURNING *;

-- name: RenameInboxFolder :one
UPDATE inbox_folder SET name = $1
WHERE id = $2 AND workspace_id = $3 AND user_id = $4
RETURNING *;

-- name: UpdateInboxFolderParent :one
-- Sets or clears the folder's parent. NULL parent = top-level. Cycle prevention
-- is enforced in application code via IsInboxFolderDescendant before update.
UPDATE inbox_folder SET parent_id = sqlc.narg('parent_id')
WHERE id = $1 AND workspace_id = $2 AND user_id = $3
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
-- Inbox-routed items (route='inbox') that are not archived and not in any folder.
-- Notifications-routed items live on a separate page; see ListNotificationsItems.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND i.route = 'inbox'
  AND NOT EXISTS (
    SELECT 1 FROM inbox_folder_membership m
    WHERE m.item_type = 'notification' AND m.item_id = i.id
  )
ORDER BY i.created_at DESC;

-- name: ListArchivedInboxItemsUnfiled :many
-- Archived inbox items not in any folder. Backs the "show archived" view in
-- the inbox kebab menu — folders show their own archived state already.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = true
  AND NOT EXISTS (
    SELECT 1 FROM inbox_folder_membership m
    WHERE m.item_type = 'notification' AND m.item_id = i.id
  )
ORDER BY i.created_at DESC;

-- name: ListArchivedChatSessionsUnfiled :many
-- Archived (creator-owned) chat sessions not in any folder. Same shape as
-- ListChatSessionsUnfiled so the unified inbox merger can reuse it.
SELECT cs.*,
       (cs.unread_since IS NOT NULL)::bool AS has_unread
FROM chat_session cs
WHERE cs.workspace_id = $1
  AND cs.creator_id = $2
  AND cs.status = 'archived'
  AND NOT EXISTS (
    SELECT 1 FROM inbox_folder_membership m
    WHERE m.item_type = 'chat_session' AND m.item_id = cs.id
  )
ORDER BY cs.updated_at DESC;

-- name: ListInboxItemsInFolder :many
-- Inbox-routed items (route='inbox') in a specific folder (regardless of
-- archived flag). Notifications-routed items can't be filed in folders.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
JOIN inbox_folder_membership m
  ON m.item_type = 'notification' AND m.item_id = i.id
JOIN inbox_folder f
  ON f.id = m.folder_id AND f.id = $1 AND f.workspace_id = $2 AND f.user_id = $3
WHERE i.route = 'inbox'
ORDER BY i.created_at DESC;

-- name: ListNotificationsItems :many
-- Notifications-routed items (route='notifications') that are not archived.
-- Mirrors ListInboxItemsUnfiled but for the lighter-weight notifications page.
SELECT i.*,
       iss.status as issue_status,
       iss.project_id as project_id
FROM inbox_item i
LEFT JOIN issue iss ON iss.id = i.issue_id
WHERE i.workspace_id = $1
  AND i.recipient_type = $2
  AND i.recipient_id = $3
  AND i.archived = false
  AND i.route = 'notifications'
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
