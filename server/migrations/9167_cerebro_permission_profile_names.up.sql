-- FIR-4269: replace migration-only UUID labels with the assigned agent's name.
--
-- Migration 9152 preserved direct agent rules as one reusable profile per
-- agent, but named every profile "Migrated agent <UUID>". The assignment
-- already points at the real agent, so the UUID is implementation detail that
-- should never have become the operator-facing name.

DO $$
DECLARE
    migrated RECORD;
    profile_name TEXT;
BEGIN
    -- Rename one profile at a time so every candidate is checked against names
    -- selected earlier. If both readable names are taken, keep the original.
    FOR migrated IN
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
        ORDER BY role.id
    LOOP
        profile_name := migrated.agent_name;
        IF migrated.agents_with_name <> 1 OR EXISTS (
            SELECT 1
            FROM cerebro_role existing
            WHERE existing.workspace_id = migrated.workspace_id
              AND existing.id <> migrated.role_id
              AND lower(existing.name) = lower(profile_name)
        ) THEN
            profile_name := migrated.agent_name || ' (' || migrated.agent_id::text || ')';
            IF EXISTS (
                SELECT 1
                FROM cerebro_role existing
                WHERE existing.workspace_id = migrated.workspace_id
                  AND existing.id <> migrated.role_id
                  AND lower(existing.name) = lower(profile_name)
            ) THEN
                CONTINUE;
            END IF;
        END IF;

        UPDATE cerebro_role role
        SET name = profile_name,
            description = CASE
                WHEN role.description = 'Automatically migrated from direct agent policy rows'
                    THEN 'Keeps the permissions that were previously configured directly for ' || migrated.agent_name || '.'
                ELSE role.description
            END,
            updated_at = now()
        WHERE role.id = migrated.role_id;
    END LOOP;
END
$$;
