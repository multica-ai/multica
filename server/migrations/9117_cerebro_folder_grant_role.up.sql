-- Collections, Phase 4 for the Notes/Documents surface (FIR-2595). The read
-- bridge cerebro_artifact_folder_grant_visible (migration 9116) answers only a
-- yes/no "can this user reach the folder". Saving a note needs more: the write
-- gate must know WHICH role the user holds, because 'viewer' may read but only
-- 'editor' / 'full_access' may edit and save.
--
-- This function returns the user's EFFECTIVE role on a folder, resolved the
-- same way as the visibility bridge: walk the folder up to the root and take
-- the strongest grant to the user across the whole chain, via
--   * the whole workspace  (grantee_type = 'workspace'),
--   * the member directly   (grantee_type = 'member'),
--   * a group the member belongs to (grantee_type = 'group').
-- A grant on ANY ancestor cascades down ("Arvet"). When the user holds several
-- grants (e.g. a direct 'viewer' plus a group 'editor'), the strongest wins.
--
-- Returns NULL when the user has no grant on the folder or its ancestors, or
-- when p_folder is NULL (a note at the workspace root has no folder to carry a
-- grant). Role ranking: full_access (3) > editor (2) > viewer (1).

CREATE OR REPLACE FUNCTION cerebro_artifact_folder_grant_role(p_folder uuid, p_user uuid)
RETURNS text
LANGUAGE sql
STABLE
AS $$
    WITH RECURSIVE chain AS (
        SELECT id, parent_id
        FROM artifact_folder
        WHERE id = p_folder
        UNION ALL
        SELECT f.id, f.parent_id
        FROM artifact_folder f
        JOIN chain c ON f.id = c.parent_id
    )
    SELECT g.role
    FROM chain
    JOIN cerebro_folder_grant g
      ON g.surface = 'artifact' AND g.folder_id = chain.id
    WHERE g.grantee_type = 'workspace'
       OR (g.grantee_type = 'member' AND g.grantee_id = p_user)
       OR (g.grantee_type = 'group' AND EXISTS (
             SELECT 1 FROM cerebro_group_member gm
             WHERE gm.group_id = g.grantee_id
               AND gm.user_id = p_user))
    ORDER BY CASE g.role
                 WHEN 'full_access' THEN 3
                 WHEN 'editor'      THEN 2
                 WHEN 'viewer'      THEN 1
                 ELSE 0
             END DESC
    LIMIT 1;
$$;
