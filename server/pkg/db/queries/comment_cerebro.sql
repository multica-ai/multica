-- CEREBRO-PATCH(comments-move-to-thread): FIR-3880 — moving comments into a new
-- thread re-parents the original rows instead of copying them and leaving a
-- breadcrumb behind, so ids, attachments, reactions and approval bindings all
-- survive the move. Net-new file; the query lives outside the upstream
-- comment.sql to keep the cerebro carve-out isolated.

-- name: SetCommentParent :one
-- Re-parents a comment. A NULL parent_id promotes the comment to a thread root.
-- updated_at is deliberately left untouched: a move is not an edit, and the
-- timeline uses updated_at to mark edited content.
UPDATE comment SET
    parent_id = sqlc.narg(parent_id)
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;
