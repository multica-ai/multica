-- Skill version snapshots (append-only). Every merged change creates a row.

-- name: ListSkillVersions :many
SELECT * FROM skill_version
WHERE skill_id = $1
ORDER BY created_at DESC;

-- name: GetSkillVersion :one
SELECT * FROM skill_version
WHERE skill_id = $1 AND version = $2;

-- name: CreateSkillVersion :one
INSERT INTO skill_version (skill_id, version, content, files, description, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;
