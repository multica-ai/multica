-- Change requests: proposed edits awaiting owner/approver review.

-- name: ListSkillChangeRequestsBySkill :many
SELECT * FROM skill_change_request
WHERE skill_id = $1
ORDER BY created_at DESC;

-- name: ListPendingChangeRequestsByWorkspace :many
SELECT cr.*
FROM skill_change_request cr
JOIN skill s ON s.id = cr.skill_id
WHERE s.workspace_id = $1 AND cr.status = 'pending'
ORDER BY cr.created_at DESC;

-- name: GetSkillChangeRequest :one
SELECT * FROM skill_change_request
WHERE id = $1;

-- name: CreateSkillChangeRequest :one
INSERT INTO skill_change_request (
    skill_id, title, description, base_version, proposed_version,
    proposed_content, proposed_files, proposed_by
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ReviewSkillChangeRequest :one
UPDATE skill_change_request SET
    status = $2,
    reviewed_by = $3,
    reviewed_at = now(),
    review_comment = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: MarkSkillChangeRequestMerged :one
UPDATE skill_change_request SET
    status = 'merged',
    updated_at = now()
WHERE id = $1
RETURNING *;
