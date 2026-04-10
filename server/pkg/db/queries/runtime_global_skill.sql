-- name: UpsertRuntimeGlobalSkill :one
INSERT INTO runtime_global_skill (runtime_id, name, description)
VALUES ($1, $2, $3)
ON CONFLICT (runtime_id, name) DO UPDATE SET
    description = EXCLUDED.description,
    updated_at  = now()
RETURNING *;

-- name: DeleteRuntimeGlobalSkillsNotIn :exec
-- Prune stale skills after a daemon re-registers with a new skill list.
DELETE FROM runtime_global_skill
WHERE runtime_id = $1
  AND name != ALL(@names::text[]);

-- name: DeleteAllRuntimeGlobalSkills :exec
DELETE FROM runtime_global_skill WHERE runtime_id = $1;

-- name: ListGlobalSkillsByWorkspace :many
-- Returns global skills for every online runtime in the workspace.
-- DISTINCT ON (daemon_id, name) prevents duplicates when a daemon registers
-- multiple runtimes (e.g. claude + codex) that all share the same global skills.
SELECT DISTINCT ON (ar.daemon_id, rgs.name)
    rgs.id, rgs.runtime_id, rgs.name, rgs.description, rgs.created_at, rgs.updated_at
FROM runtime_global_skill rgs
JOIN agent_runtime ar ON ar.id = rgs.runtime_id
WHERE ar.workspace_id = $1
  AND ar.status = 'online'
ORDER BY ar.daemon_id, rgs.name ASC;
