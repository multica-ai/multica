-- FIR-3315: app folders are Collections and share the same direct/inherited
-- role grants as Documents, Notes, Skills, and Autopilots.
ALTER TABLE cerebro_folder_grant
    DROP CONSTRAINT IF EXISTS cerebro_folder_grant_surface_check;

ALTER TABLE cerebro_folder_grant
    ADD CONSTRAINT cerebro_folder_grant_surface_check
    CHECK (surface IN ('artifact', 'entity', 'app'));

CREATE OR REPLACE FUNCTION cerebro_app_folder_grant_visible(p_folder uuid, p_user uuid)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    WITH RECURSIVE chain AS (
        SELECT id, parent_id
        FROM cerebro_app_folder
        WHERE id = p_folder
        UNION ALL
        SELECT f.id, f.parent_id
        FROM cerebro_app_folder f
        JOIN chain c ON f.id = c.parent_id
    )
    SELECT EXISTS (
        SELECT 1
        FROM chain
        JOIN cerebro_folder_grant g
          ON g.surface = 'app' AND g.folder_id = chain.id
        WHERE g.grantee_type = 'workspace'
           OR (g.grantee_type = 'member' AND g.grantee_id = p_user)
           OR (g.grantee_type = 'group' AND EXISTS (
                 SELECT 1 FROM cerebro_group_member gm
                 WHERE gm.group_id = g.grantee_id
                   AND gm.user_id = p_user))
    );
$$;
