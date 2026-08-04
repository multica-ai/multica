-- Restore untouched automatically migrated profiles. Profiles renamed or
-- edited by a user after the up migration are deliberately preserved.

WITH renamed_profiles AS (
    SELECT role.id AS role_id, assigned_agent.id AS agent_id
    FROM cerebro_role role
    JOIN cerebro_role_assignment assignment
      ON assignment.role_id = role.id
     AND assignment.subject_type = 'agent'
    JOIN agent assigned_agent
      ON assigned_agent.id = assignment.subject_id
     AND assigned_agent.workspace_id = role.workspace_id
    WHERE role.description = 'Keeps the permissions that were previously configured directly for ' || btrim(assigned_agent.name) || '.'
      AND role.name IN (
          btrim(assigned_agent.name),
          btrim(assigned_agent.name) || ' (' || assigned_agent.id::text || ')'
      )
)
UPDATE cerebro_role role
SET name = 'Migrated agent ' || renamed.agent_id::text,
    description = 'Automatically migrated from direct agent policy rows',
    updated_at = now()
FROM renamed_profiles renamed
WHERE role.id = renamed.role_id
  AND NOT EXISTS (
      SELECT 1
      FROM cerebro_role existing
      WHERE existing.workspace_id = role.workspace_id
        AND existing.id <> role.id
        AND existing.name = 'Migrated agent ' || renamed.agent_id::text
  );
