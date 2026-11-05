-- Squad skills and plugins. Every query filters by workspace_id and squad_id.

-- name: ListSquadSkills :many
SELECT s.id, s.name, s.description
FROM skill s
JOIN squad_skill ss ON ss.skill_id = s.id
WHERE ss.workspace_id = $1 AND ss.squad_id = $2
ORDER BY s.name ASC;

-- name: DeleteSquadSkills :exec
DELETE FROM squad_skill WHERE workspace_id = $1 AND squad_id = $2;

-- name: AddSquadSkill :exec
INSERT INTO squad_skill (workspace_id, squad_id, skill_id, created_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT DO NOTHING;

-- name: ListSquadPlugins :many
SELECT plugin_name, install_hint
FROM squad_plugin
WHERE workspace_id = $1 AND squad_id = $2
ORDER BY plugin_name ASC;

-- name: DeleteSquadPlugins :exec
DELETE FROM squad_plugin WHERE workspace_id = $1 AND squad_id = $2;

-- name: AddSquadPlugin :exec
INSERT INTO squad_plugin (workspace_id, squad_id, plugin_name, install_hint, created_by)
VALUES ($1, $2, $3, $4, $5);

-- name: ListEffectiveAgentSkills :many
-- The skills an agent runs with: its own enabled skills plus the skills of every
-- live squad it is a member of. Claim and skill-bundle resolve both read this,
-- so a squad skill reaches every member on every task. The agent's skill list
-- endpoints keep reading ListAgentSkills, which only shows direct attachments.
SELECT s.* FROM skill s
WHERE s.id IN (
    SELECT ask.skill_id FROM agent_skill ask
    WHERE ask.agent_id = $1 AND ask.enabled = TRUE
    UNION
    SELECT ss.skill_id FROM squad_skill ss
    JOIN squad sq ON sq.id = ss.squad_id AND sq.archived_at IS NULL
    JOIN squad_member sm ON sm.squad_id = ss.squad_id
        AND sm.member_type = 'agent' AND sm.member_id = $1
)
ORDER BY s.name ASC;

-- name: ListEffectiveAgentSkillsByIDs :many
-- Scoped variant of ListEffectiveAgentSkills for the skill-bundle resolve path.
-- The same membership predicate is the authorization: an ID the agent cannot
-- reach returns no row.
SELECT s.* FROM skill s
WHERE s.id = ANY(sqlc.arg('skill_ids')::uuid[])
  AND s.id IN (
    SELECT ask.skill_id FROM agent_skill ask
    WHERE ask.agent_id = $1 AND ask.enabled = TRUE
    UNION
    SELECT ss.skill_id FROM squad_skill ss
    JOIN squad sq ON sq.id = ss.squad_id AND sq.archived_at IS NULL
    JOIN squad_member sm ON sm.squad_id = ss.squad_id
        AND sm.member_type = 'agent' AND sm.member_id = $1
  )
ORDER BY s.name ASC;
