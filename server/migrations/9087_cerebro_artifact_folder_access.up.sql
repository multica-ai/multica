-- Folder-level access control (FIR-1590). Today an artifact folder (note or
-- document surface) is workspace-shared: everyone sees every folder. Per-note
-- access already exists via cerebro_note.visibility; this brings the same
-- "Only you / Selected colleagues / Whole team" choice to the folder itself.
--
-- Mirrors the cerebro_note model exactly:
--   owner_id    — the member who owns the folder (creator). NULL for legacy
--                 folders created before this feature; they behave as
--                 'workspace' and are never private/shared.
--   visibility  — who may see the folder AND its contents:
--                   'private'   = only the owner,
--                   'shared'    = owner + users in cerebro_artifact_folder_share,
--                   'workspace' = every member (default — preserves today's
--                                 behavior for all existing folders).
--
-- Decision (Jesper, FIR-1590): setting access on a folder locks the WHOLE
-- folder at once — the restriction gates the folder and everything inside it
-- (notes, documents, subfolders). Composition is most-restrictive-wins: a
-- folder can only tighten access to its contents, never widen it, so a private
-- note placed in a workspace folder stays private.

ALTER TABLE artifact_folder
    ADD COLUMN IF NOT EXISTS owner_id uuid;

ALTER TABLE artifact_folder
    ADD COLUMN IF NOT EXISTS visibility text NOT NULL DEFAULT 'workspace'
        CHECK (visibility IN ('private', 'shared', 'workspace'));

CREATE INDEX IF NOT EXISTS cerebro_artifact_folder_owner_idx
    ON artifact_folder (owner_id);

-- Explicit per-user shares, only meaningful when visibility = 'shared'.
CREATE TABLE IF NOT EXISTS cerebro_artifact_folder_share (
    folder_id  uuid        NOT NULL REFERENCES artifact_folder(id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_id, user_id)
);

CREATE INDEX IF NOT EXISTS cerebro_artifact_folder_share_user_idx
    ON cerebro_artifact_folder_share (user_id);

-- Single source of truth for "can this user see this folder (and therefore
-- anything inside it)". Walks the folder up to the root: the user must pass
-- the access check of the folder AND every ancestor. Used by the folder list,
-- the notes list, and the documents list so enforcement is identical
-- everywhere. p_folder NULL (an item at the workspace root, no folder) is
-- always visible — there is no folder to gate it.
CREATE OR REPLACE FUNCTION cerebro_artifact_folder_visible(p_folder uuid, p_user uuid)
RETURNS boolean
LANGUAGE sql
STABLE
AS $$
    WITH RECURSIVE chain AS (
        SELECT id, parent_id, visibility, owner_id
        FROM artifact_folder
        WHERE id = p_folder
        UNION ALL
        SELECT f.id, f.parent_id, f.visibility, f.owner_id
        FROM artifact_folder f
        JOIN chain c ON f.id = c.parent_id
    )
    SELECT COALESCE(bool_and(
        chain.visibility = 'workspace'
        OR chain.owner_id = p_user
        OR (chain.visibility = 'shared' AND EXISTS (
            SELECT 1 FROM cerebro_artifact_folder_share s
            WHERE s.folder_id = chain.id AND s.user_id = p_user))
    ), true)
    FROM chain;
$$;
