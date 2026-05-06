-- Skill forks: directional links between a parent skill and a forked variant.

-- name: ListSkillForksByParent :many
SELECT sf.*, s.name AS forked_name, s.description AS forked_description, s.current_version
FROM skill_fork sf
JOIN skill s ON s.id = sf.forked_skill_id
WHERE sf.parent_skill_id = $1
ORDER BY sf.created_at DESC;

-- name: GetSkillForkParent :one
SELECT sf.*, s.name AS parent_name
FROM skill_fork sf
JOIN skill s ON s.id = sf.parent_skill_id
WHERE sf.forked_skill_id = $1;

-- name: CreateSkillFork :one
INSERT INTO skill_fork (parent_skill_id, forked_skill_id, forked_by)
VALUES ($1, $2, $3)
RETURNING *;
