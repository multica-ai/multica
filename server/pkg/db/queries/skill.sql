-- Skill CRUD

-- name: ListSkillsByWorkspace :many
SELECT * FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: ListSkillSummariesByWorkspace :many
-- Same as ListSkillsByWorkspace but omits the SKILL.md `content` column. Used
-- by list endpoints (CLI table, web list page) where the body is never read;
-- shipping it everywhere blew up payload size on workspaces with many skills
-- and caused 15s CLI timeouts from high-latency regions (GH multica-ai/multica#2174).
SELECT id, workspace_id, name, description, config, created_by, created_at, updated_at
FROM skill
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: GetSkill :one
SELECT * FROM skill
WHERE id = $1;

-- name: GetSkillInWorkspace :one
SELECT * FROM skill
WHERE id = $1 AND workspace_id = $2;

-- name: GetSkillByWorkspaceAndName :one
-- Used by skill import and runtime-local skill discovery to reuse a workspace
-- skill by name rather than violating UNIQUE(workspace_id, name).
SELECT * FROM skill
WHERE workspace_id = $1 AND name = $2;

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
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
DELETE FROM skill WHERE id = $1 AND workspace_id = $2;

-- Skill File CRUD

-- name: ListSkillFiles :many
SELECT * FROM skill_file
WHERE skill_id = $1
ORDER BY path ASC;

-- name: ListSkillFilesBySkillIDs :many
-- Batch variant of ListSkillFiles: loads every file for a set of skills in one
-- round trip so LoadAgentSkills doesn't issue one query per skill on the
-- task-claim hot path. Ordered by skill_id so the caller can group in a single
-- linear pass. Like ListSkillFiles it returns full file bodies — callers that
-- only need metadata must use ListSkillFileMetadata instead. Uses
-- idx_skill_file_skill.
SELECT * FROM skill_file
WHERE skill_id = ANY(sqlc.arg('skill_ids')::uuid[])
ORDER BY skill_id, path ASC;

-- name: ListSkillFileMetadata :many
-- Metadata-only variant of ListSkillFiles: path, byte size and content hash
-- without the body. Same reason as ListSkillSummariesByWorkspace — a skill
-- whose supporting files total ~600KB cannot be listed at all when every row
-- carries its full content, and the one command that would show which file is
-- oversized was the command that timed out (GH multica-ai/multica#7498).
-- size/hash are computed in Postgres so the file bodies never leave it.
--
-- convert_to(content, 'UTF8'), never content::bytea: the cast runs the bytea
-- INPUT parser over the text, so it reads backslash escapes instead of taking
-- the bytes. A file containing `\x41` would hash as the single byte `A`, and
-- one containing a bare backslash — a regex `\d+`, a Windows path, a LaTeX
-- snippet — fails outright with "invalid input syntax for type bytea",
-- turning an ordinary skill into a 500 on this endpoint.
SELECT id, skill_id, path,
       octet_length(content)::bigint AS size,
       encode(sha256(convert_to(content, 'UTF8')), 'hex') AS content_hash,
       created_at, updated_at
FROM skill_file
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
WHERE ask.agent_id = $1 AND ask.enabled = TRUE
ORDER BY s.name ASC;

-- name: ListAgentSkillsByIDs :many
-- Scoped variant of ListAgentSkills: the same junction predicate, narrowed to
-- a set of requested skill IDs. The skill-bundle resolve path asks for one
-- skill per request, so loading the agent's whole set there costs a full read
-- and hash of every skill on every request. The junction predicate is also the
-- authorization: an ID the agent does not have enabled simply returns no row,
-- which the caller reports as not-found.
SELECT s.* FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1
  AND ask.enabled = TRUE
  AND s.id = ANY(sqlc.arg('skill_ids')::uuid[])
ORDER BY s.name ASC;

-- name: ListAgentSkillSummaries :many
-- Summary variant for the agent skills list endpoint — omits `content` for
-- the same reason as ListSkillSummariesByWorkspace.
SELECT s.id, s.workspace_id, s.name, s.description, s.config, s.created_by, s.created_at, s.updated_at, ask.enabled
FROM skill s
JOIN agent_skill ask ON ask.skill_id = s.id
WHERE ask.agent_id = $1
ORDER BY s.name ASC;

-- name: ListAgentSkillNamesByAgentIDs :many
SELECT ask.agent_id, s.name
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE ask.agent_id = ANY(sqlc.arg('agent_ids')::uuid[])
  AND ask.enabled = TRUE
ORDER BY ask.agent_id, s.name ASC;

-- name: AddAgentSkill :exec
INSERT INTO agent_skill (agent_id, skill_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: SetAgentSkillEnabled :execrows
UPDATE agent_skill
SET enabled = $3
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skill
WHERE agent_id = $1 AND skill_id = $2;

-- name: RemoveAllAgentSkills :exec
DELETE FROM agent_skill WHERE agent_id = $1;

-- name: ListAgentSkillsByWorkspace :many
SELECT ask.agent_id, s.id, s.name, s.description, ask.enabled
FROM agent_skill ask
JOIN skill s ON s.id = ask.skill_id
WHERE s.workspace_id = $1
ORDER BY s.name ASC;
