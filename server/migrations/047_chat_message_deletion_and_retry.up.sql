-- Add support for deleting chat messages and retrying messages.

-- Get a single chat message by ID.
-- name: GetChatMessage :one
SELECT * FROM chat_message WHERE id = $1;

-- Delete a chat message by ID.
-- Note: task_message table has task_id foreign key ON DELETE CASCADE,
-- so associated task_message rows are automatically deleted.
-- name: DeleteChatMessage :exec
DELETE FROM chat_message WHERE id = $1;
