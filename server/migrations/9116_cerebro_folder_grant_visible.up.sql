-- Collections, Phase 3 for the Notes/Documents surface (FIR-2595). Bridges the
-- NEW per-folder grant model (cerebro_folder_grant, migration 9099) into note
-- ACCESS resolution. Until now the "Access" dialog wrote cerebro_folder_grant
-- rows but nothing in note-open read them, so a user granted "Full access" in
-- the dialog still got "note not found" (FIR-2589 root cause: two disconnected
-- permission systems).
--
-- This function is the read side of that bridge. It answers "does this user
-- reach the folder (and therefore its notes) via a Collections grant?" by
-- walking the folder up to the root and checking each ancestor for a grant to:
--   * the whole workspace  (grantee_type = 'workspace'),
--   * the member directly  (grantee_type = 'member'),
--   * a group the member belongs to (grantee_type = 'group').
-- A grant on ANY ancestor cascades down — that is the "Arvet" (inherited)
-- behaviour of the approved Collections design.
--
-- ADDITIVE ROLLOUT (mirrors the project-grant cutover, FIR-2125):
--   The note queries OR this function alongside the legacy
--   cerebro_artifact_folder_visible check. Grants only ever ADD access; nobody
--   loses the visibility they have today. The legacy path stays fully in
--   effect. A later phase can backfill legacy shares into grants and then drop
--   the old path once the additive model is proven in production.
--
-- p_folder NULL (a note at the workspace root, no folder) yields false here —
-- there is no folder to carry a grant — and the legacy OR-branch still governs
-- root notes, so root behaviour is unchanged.

CREATE OR REPLACE FUNCTION cerebro_artifact_folder_grant_visible(p_folder uuid, p_user uuid)
RETURNS boolean
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
    SELECT EXISTS (
        SELECT 1
        FROM chain
        JOIN cerebro_folder_grant g
          ON g.surface = 'artifact' AND g.folder_id = chain.id
        WHERE g.grantee_type = 'workspace'
           OR (g.grantee_type = 'member' AND g.grantee_id = p_user)
           OR (g.grantee_type = 'group' AND EXISTS (
                 SELECT 1 FROM cerebro_group_member gm
                 WHERE gm.group_id = g.grantee_id
                   AND gm.user_id = p_user))
    );
$$;
