-- Skill sharing scope. Skill ownership remains at the Workspace level.

-- name: ListSkillAvailableSpaces :many
SELECT * FROM skill_available_space
WHERE skill_id = $1
ORDER BY created_at ASC, space_id ASC;

-- name: ListSkillAvailableSpacesBySkillIDs :many
SELECT * FROM skill_available_space
WHERE skill_id = ANY(@skill_ids::uuid[])
ORDER BY skill_id ASC, created_at ASC, space_id ASC;

-- name: ReplaceSkillAvailableSpaces :exec
WITH deleted AS (
    DELETE FROM skill_available_space
    WHERE skill_id = @skill_id
      AND NOT (
          space_id = ANY(COALESCE(@space_ids::uuid[], ARRAY[]::uuid[]))
      )
)
INSERT INTO skill_available_space (
    skill_id, workspace_id, space_id, created_by
)
SELECT
    @skill_id, @workspace_id, selected.space_id, sqlc.narg('created_by')
FROM unnest(COALESCE(@space_ids::uuid[], ARRAY[]::uuid[])) AS selected(space_id)
ON CONFLICT (skill_id, space_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    created_by = EXCLUDED.created_by,
    created_at = now();

-- name: UpdateSkillAvailability :one
UPDATE skill
SET availability_mode = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IsSkillAvailableInActiveSpace :one
SELECT EXISTS (
    SELECT 1
    FROM skill_available_space sas
    JOIN skill s
      ON s.workspace_id = sas.workspace_id
     AND s.id = sas.skill_id
    JOIN workspace_space ws
      ON ws.workspace_id = sas.workspace_id
     AND ws.id = sas.space_id
    WHERE sas.skill_id = $1
      AND sas.workspace_id = $2
      AND sas.space_id = $3
      AND ws.archived_at IS NULL
)::boolean;
