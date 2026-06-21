-- name: ListCerebroSessionsForIssue :many
SELECT * FROM cerebro_session
WHERE issue_id = @issue_id
ORDER BY position ASC, created_at ASC, id ASC;

-- name: GetCerebroSessionForIssue :one
SELECT * FROM cerebro_session
WHERE id = @id AND issue_id = @issue_id;

-- name: GetOpenCerebroSessionForIssue :one
SELECT * FROM cerebro_session
WHERE issue_id = @issue_id AND status <> 'done'
ORDER BY position DESC, created_at DESC, id DESC
LIMIT 1;

-- name: GetNextCerebroSessionPosition :one
SELECT COALESCE(MAX(position), 0)::int + 1
FROM cerebro_session
WHERE issue_id = @issue_id;

-- name: CountCerebroSessionsForIssue :one
SELECT COUNT(*)::int FROM cerebro_session
WHERE issue_id = @issue_id;

-- name: CreateCerebroSession :one
INSERT INTO cerebro_session (
    issue_id,
    position,
    name,
    status,
    handoff,
    created_at
) VALUES (
    @issue_id,
    @position,
    @name,
    @status,
    sqlc.narg(handoff),
    COALESCE(sqlc.narg(created_at), now())
)
RETURNING *;

-- name: UpdateCerebroSessionName :one
UPDATE cerebro_session
SET name = @name, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroSessionStatus :one
UPDATE cerebro_session
SET status = @status, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroSessionHandoff :one
UPDATE cerebro_session
SET handoff = sqlc.narg(handoff), updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: CloseCerebroSession :one
UPDATE cerebro_session
SET status = 'done', updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;
