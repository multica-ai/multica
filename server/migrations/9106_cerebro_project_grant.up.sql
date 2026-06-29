-- Collections, Phase 2 (FIR-2125): hierarchical project access grants.
-- Mirrors cerebro_folder_grant (9099) but wired to the project tree
-- (project_nesting) instead of the folder trees (artifact_folder /
-- cerebro_entity_folder). A grant on a project cascades down to its
-- sub-projects via WITH RECURSIVE at read time; no inheritance is stored.
--
-- Grantee types and roles are identical to cerebro_folder_grant so the
-- same FolderAccessEditor UI component can be reused on the project surface.
--
-- The table is gated client-side by the cerebro_collections feature flag
-- (OFF by default); these endpoints stay dormant until the flag and UI ship.
--
-- CUTOVER PLAN (FIR-2125 — additive rollout):
--   Phase 1 (this migration): new table + additive OR-clause in
--     ListProjectsAccessibleToUser. Old model (project.access / project_member /
--     cerebro_project_group_member) stays fully in effect. New grants are additive.
--   Phase 2 (future migration): backfill — convert existing access rows to grants:
--     * project.access = 'workspace'           → INSERT grantee_type='workspace'
--     * project_member rows                    → INSERT grantee_type='member'
--     * cerebro_project_group_member rows      → INSERT grantee_type='group'
--   Phase 3 (future migration): remove old model (project.access, project_member,
--     cerebro_project_group_member) and drop the old OR-clauses from SQL queries.
--
-- CONFLICT / INHERITANCE RULES:
--   1. Visibility: union of old model + new model (either path grants access).
--   2. Effective role: among all grant rows touching a member (direct, group,
--      workspace, inherited from ancestor projects) the HIGHEST role wins.
--      Precedence: full_access > editor > viewer.
--   3. Specificity: a direct grant on a child project always wins over an
--      inherited grant from a parent for the same grantee (depth=0 is always
--      preferred in the UI "This project" tab).
--   4. Source: the listEffectiveGrants query returns rows ordered by depth ASC,
--      so the UI shows the most direct source first. No server-side dedup is
--      done; the client (ProjectGrantsPanel) chooses the highest role.

CREATE TABLE IF NOT EXISTS cerebro_project_grant (
    id           uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    workspace_id uuid        NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   uuid        NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    grantee_type text        NOT NULL CHECK (grantee_type IN ('group', 'member', 'workspace', 'agent', 'runtime')),
    grantee_id   uuid,
    role         text        NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer', 'editor', 'full_access')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   uuid,
    -- 'workspace' is the only grantee with no id; everyone else must name one.
    CONSTRAINT cerebro_project_grant_workspace_grantee_chk
        CHECK ((grantee_type = 'workspace') = (grantee_id IS NULL))
);

-- One grant row per (project, grantee). COALESCE folds the single NULL-id
-- 'workspace' grant into the uniqueness key so a project cannot carry the
-- "Whole team" grant twice.
CREATE UNIQUE INDEX IF NOT EXISTS cerebro_project_grant_unique_idx
    ON cerebro_project_grant (
        project_id,
        grantee_type,
        COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid)
    );

-- Read path: "all direct grants on this project" (the "This project" tab).
CREATE INDEX IF NOT EXISTS cerebro_project_grant_project_idx
    ON cerebro_project_grant (workspace_id, project_id);

-- Read path: "everything granted to this group/member/agent/…" — for
-- resolving what a given viewer may see across all projects.
CREATE INDEX IF NOT EXISTS cerebro_project_grant_grantee_idx
    ON cerebro_project_grant (grantee_type, grantee_id);
