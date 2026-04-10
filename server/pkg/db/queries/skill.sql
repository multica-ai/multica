-- Skill CRUD

-- name: ListSkillsByWorkspace :many
SELECT * FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: GetSkill :one
SELECT * FROM skill
WHERE id = $1;

-- name: GetSkillInWorkspace :one
SELECT * FROM skill
WHERE id = $1 AND workspace_id = $2;

-- name: CreateSkill :one
INSERT INTO skill (workspace_id, name, description, content, config, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateSkill :one
UPDATE skill SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    content = COALESCE(sqlc.narg('content'), content),
    config = COALESCE(sqlc.narg('config'), config),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteSkill :exec
DELETE FROM skill WHERE id = $1;

-- Global skill queries (daemon-owned, is_global = true)

-- name: UpsertGlobalSkill :one
INSERT INTO skill (workspace_id, runtime_id, name, description, content, config, is_global)
VALUES ($1, $2, $3, $4, $5, '{}', true)
ON CONFLICT (runtime_id, name) WHERE is_global DO UPDATE SET
    description = EXCLUDED.description,
    content     = EXCLUDED.content,
    updated_at  = now()
RETURNING *;

-- name: DeleteGlobalSkillsNotIn :exec
DELETE FROM skill
WHERE runtime_id = $1 AND is_global = true
  AND name != ALL(@names::text[]);

-- name: DeleteAllGlobalSkillsByRuntime :exec
DELETE FROM skill WHERE runtime_id = $1 AND is_global = true;

-- Skill File CRUD

-- name: ListSkillFiles :many
SELECT * FROM skill_file
WHERE skill_id = $1
ORDER BY path ASC;

-- name: GetSkillFile :one
SELECT * FROM skill_file
WHERE id = $1;

-- name: UpsertSkillFile :one
INSERT INTO skill_file (skill_id, path, content)
VALUES ($1, $2, $3)
ON CONFLICT (skill_id, path) DO UPDATE SET
    content = EXCLUDED.content,
    updated_at = now()
RETURNING *;

-- name: DeleteSkillFile :exec
DELETE FROM skill_file WHERE id = $1;

-- name: DeleteSkillFilesBySkill :exec
DELETE FROM skill_file WHERE skill_id = $1;

-- Agent-Skill junction

-- name: ListAgentSkills :many
SELECT s.* FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1
ORDER BY s.name ASC;

-- name: AddAgentSkill :exec
INSERT INTO agent_skill (agent_id, skill_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skill
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAllAgentSkills :exec
DELETE FROM agent_skill WHERE agent_id = $1;

-- name: ListAgentSkillsByWorkspace :many
SELECT ask.agent_id, s.id, s.name, s.description, s.is_global, s.runtime_id
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = $1
ORDER BY s.name ASC;
