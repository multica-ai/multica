-- FIR-4269: replace migration-only UUID labels with the assigned agent's name.
--
-- Migration 9152 preserved direct agent rules as one reusable profile per
-- agent, but named every profile "Migrated agent <UUID>". The assignment
-- already points at the real agent, so the UUID is implementation detail that
-- should never have become the operator-facing name.

WITH migrated_profiles AS (
    SELECT
        role.id AS role_id,
        role.workspace_id,
        assigned_agent.id AS agent_id,
        btrim(assigned_agent.name) AS agent_name,
        count(*) OVER (
            PARTITION BY role.workspace_id, lower(btrim(assigned_agent.name))
        ) AS agents_with_name
    FROM cerebro_role role
    JOIN cerebro_role_assignment assignment
      ON assignment.role_id = role.id
     AND assignment.subject_type = 'agent'
    JOIN agent assigned_agent
      ON assigned_agent.id = assignment.subject_id
     AND assigned_agent.workspace_id = role.workspace_id
    WHERE role.name = 'Migrated agent ' || assigned_agent.id::text
      AND btrim(assigned_agent.name) <> ''
), resolved_names AS (
    SELECT
        migrated.role_id,
        migrated.agent_name,
        CASE
            WHEN migrated.agents_with_name = 1
             AND NOT EXISTS (
                 SELECT 1
                 FROM cerebro_role existing
                 WHERE existing.workspace_id = migrated.workspace_id
                   AND existing.id <> migrated.role_id
                   AND lower(existing.name) = lower(migrated.agent_name)
             )
                THEN migrated.agent_name
            ELSE migrated.agent_name || ' (' || migrated.agent_id::text || ')'
        END AS profile_name
    FROM migrated_profiles migrated
)
UPDATE cerebro_role role
SET name = resolved.profile_name,
    description = CASE
        WHEN role.description = 'Automatically migrated from direct agent policy rows'
            THEN 'Keeps the permissions that were previously configured directly for ' || resolved.agent_name || '.'
        ELSE role.description
    END,
    updated_at = now()
FROM resolved_names resolved
WHERE role.id = resolved.role_id;
