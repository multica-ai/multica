-- FIR-1874: a session is a thread. A cerebro_session row is optional metadata
-- (name/headline + handoff) keyed to the thread root via root_comment_id. State
-- (open/closed) is NOT stored here — it is the thread root's resolved_at. There
-- is no status column and no position-driven membership anymore.

-- name: ListCerebroSessionsForIssue :many
SELECT * FROM cerebro_session
WHERE issue_id = @issue_id
ORDER BY created_at ASC, id ASC;

-- name: GetCerebroSessionForIssue :one
SELECT * FROM cerebro_session
WHERE id = @id AND issue_id = @issue_id;

-- name: GetCerebroSessionByRootComment :one
SELECT * FROM cerebro_session
WHERE issue_id = @issue_id AND root_comment_id = @root_comment_id;

-- name: CreateCerebroSession :one
INSERT INTO cerebro_session (
    issue_id,
    root_comment_id,
    position,
    name,
    handoff,
    created_at
) VALUES (
    @issue_id,
    sqlc.narg(root_comment_id),
    @position,
    @name,
    sqlc.narg(handoff),
    COALESCE(sqlc.narg(created_at), now())
)
RETURNING *;

-- name: UpdateCerebroSessionName :one
UPDATE cerebro_session
SET name = @name, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroSessionHandoff :one
UPDATE cerebro_session
SET handoff = sqlc.narg(handoff), updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;
