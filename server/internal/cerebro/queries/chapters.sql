-- name: ListCerebroChaptersForIssue :many
SELECT * FROM cerebro_chapters
WHERE issue_id = @issue_id
ORDER BY position ASC, created_at ASC, id ASC;

-- name: GetCerebroChapterForIssue :one
SELECT * FROM cerebro_chapters
WHERE id = @id AND issue_id = @issue_id;

-- name: GetOpenCerebroChapterForIssue :one
SELECT * FROM cerebro_chapters
WHERE issue_id = @issue_id AND status <> 'done'
ORDER BY position DESC, created_at DESC, id DESC
LIMIT 1;

-- name: GetNextCerebroChapterPosition :one
SELECT COALESCE(MAX(position), 0)::int + 1
FROM cerebro_chapters
WHERE issue_id = @issue_id;

-- name: CreateCerebroChapter :one
INSERT INTO cerebro_chapters (
    issue_id,
    name,
    status,
    position,
    handoff_summary,
    handoff_done,
    handoff_remaining,
    plan_ref
) VALUES (
    @issue_id,
    @name,
    @status,
    @position,
    @handoff_summary,
    @handoff_done,
    @handoff_remaining,
    sqlc.narg(plan_ref)
)
RETURNING *;

-- name: UpdateCerebroChapterName :one
UPDATE cerebro_chapters
SET name = @name, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroChapterStatus :one
UPDATE cerebro_chapters
SET status = @status, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroChapterPosition :one
UPDATE cerebro_chapters
SET position = @position, updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: UpdateCerebroChapterHandoff :one
UPDATE cerebro_chapters
SET
    handoff_summary = @handoff_summary,
    handoff_done = @handoff_done,
    handoff_remaining = @handoff_remaining,
    plan_ref = sqlc.narg(plan_ref),
    updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;

-- name: CloseCerebroChapter :one
UPDATE cerebro_chapters
SET status = 'done', updated_at = now()
WHERE id = @id AND issue_id = @issue_id
RETURNING *;
