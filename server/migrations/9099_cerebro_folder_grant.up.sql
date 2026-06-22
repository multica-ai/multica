-- Collections, Phase 1 (FIR-1590 → Collections). Foundation only: the
-- per-folder ACCESS GRANT model that the "Valgt her / Arvet" editor in the
-- approved design reads and writes. No behaviour is wired yet; nothing reads
-- this table until the resolution + handlers land in the next increment, and
-- the surface is gated by the OFF feature flag `cerebro_collections`.
--
-- Why a single polymorphic grant table:
--   The approved design puts the SAME access editor on every foldered surface
--   — Documents + Notes (artifact_folder) and Autopilots + Skills
--   (cerebro_entity_folder). One table keyed by (surface, folder_id) lets the
--   resolution logic and the UI be written once instead of four times. Because
--   it spans two physical folder tables there is no folder_id FK; orphan rows
--   are cleaned up by the delete handlers (and a follow-up FK-by-trigger may be
--   added per surface).
--
-- Grantee types (who a folder can be shared with) and roles come straight from
-- the approved mockup:
--   grantee_type: group | member | workspace | agent | runtime
--     - 'workspace' is the "Whole team" grant; it has no grantee_id.
--     - agent / runtime are in the model from day one (they show up in the
--       inherited list in the design) even though the add-UI for them is a
--       later phase — so no migration is needed to enable them.
--   role: viewer | editor | full_access  (Viewer / Editor / Full access)
--
-- Inheritance ("Arvet") is NOT stored: a grant set on a folder applies to that
-- folder and cascades down to its descendants. It is computed by walking the
-- folder's ancestors at read time (next increment), so a child folder shows
-- ancestor grants under "Arvet" and may "Override" with its own direct rows.

CREATE TABLE IF NOT EXISTS cerebro_folder_grant (
    id           uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    surface      text        NOT NULL CHECK (surface IN ('artifact', 'entity')),
    folder_id    uuid        NOT NULL,
    grantee_type text        NOT NULL CHECK (grantee_type IN ('group', 'member', 'workspace', 'agent', 'runtime')),
    grantee_id   uuid,
    role         text        NOT NULL DEFAULT 'viewer' CHECK (role IN ('viewer', 'editor', 'full_access')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    created_by   uuid,
    -- 'workspace' is the only grantee with no id; everyone else must name one.
    CONSTRAINT cerebro_folder_grant_workspace_id_chk
        CHECK ((grantee_type = 'workspace') = (grantee_id IS NULL))
);

-- One grant row per (folder, grantee). COALESCE folds the single NULL-id
-- 'workspace' grant into the uniqueness key so a folder cannot carry the
-- "Whole team" grant twice.
CREATE UNIQUE INDEX IF NOT EXISTS cerebro_folder_grant_unique_idx
    ON cerebro_folder_grant (
        surface,
        folder_id,
        grantee_type,
        COALESCE(grantee_id, '00000000-0000-0000-0000-000000000000'::uuid)
    );

-- Read path: "all direct grants on this folder" (the Valgt her tab) and the
-- ancestor walk that powers Arvet.
CREATE INDEX IF NOT EXISTS cerebro_folder_grant_folder_idx
    ON cerebro_folder_grant (surface, folder_id);

-- Read path: "everything granted to this group/member/agent/…" used when
-- resolving what a given viewer may see.
CREATE INDEX IF NOT EXISTS cerebro_folder_grant_grantee_idx
    ON cerebro_folder_grant (grantee_type, grantee_id);
