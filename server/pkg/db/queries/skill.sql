-- Skill CRUD

-- name: ListSkillsByWorkspace :many
SELECT * FROM skill
WHERE workspace_id = ?
ORDER BY name ASC;

-- name: GetSkill :one
SELECT * FROM skill
WHERE id = ?;

-- name: GetSkillInWorkspace :one
SELECT * FROM skill
WHERE id = ? AND workspace_id = ?;

-- name: CreateSkill :one
INSERT INTO skill (id, workspace_id, name, description, content, config, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateSkill :one
UPDATE skill SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = datetime('now')
WHERE id = ?
RETURNING *;

-- name: DeleteSkill :exec
DELETE FROM skill WHERE id = ?;

-- Skill File CRUD

-- name: ListSkillFiles :many
SELECT * FROM skill_file
WHERE skill_id = ?
ORDER BY path ASC;

-- name: GetSkillFile :one
SELECT * FROM skill_file
WHERE id = ?;

-- name: UpsertSkillFile :one
INSERT INTO skill_file (id, skill_id, path, content)
VALUES (?, ?, ?, ?)
ON CONFLICT (skill_id, path) DO UPDATE SET
    content = EXCLUDED.content,
    updated_at = datetime('now')
RETURNING *;

-- name: DeleteSkillFile :exec
DELETE FROM skill_file WHERE id = ?;

-- name: DeleteSkillFilesBySkill :exec
DELETE FROM skill_file WHERE skill_id = ?;

-- Agent-Skill junction

-- name: ListAgentSkills :many
SELECT s.* FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = ?
ORDER BY s.name ASC;

-- name: AddAgentSkill :exec
INSERT INTO agent_skill (agent_id, skill_id)
VALUES (?, ?)
ON CONFLICT DO NOTHING;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skill
WHERE agent_id = ? AND skill_id = ?;

-- name: RemoveAllAgentSkills :exec
DELETE FROM agent_skill WHERE agent_id = ?;

-- name: ListAgentSkillsByWorkspace :many
SELECT ask.agent_id, s.id, s.name, s.description
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = ?
ORDER BY s.name ASC;
