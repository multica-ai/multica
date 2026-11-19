-- Project folders (MUL-9390): a one-level, workspace-wide grouping of
-- projects. A project sits in at most one folder (project.folder_id); NULL
-- means it is in none. Folders do not nest and are not an access boundary: a
-- private project stays visible only to its members wherever it is filed.
--
-- No foreign keys (repo rule): DeleteProjectFolder clears project.folder_id in
-- the same transaction, and workspace teardown removes the folders with the
-- projects. Clients treat a folder_id they cannot resolve as no folder.
--
-- The name index is built concurrently in 682.
CREATE TABLE IF NOT EXISTS project_folder (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL,
    creator_type TEXT,
    creator_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE project ADD COLUMN IF NOT EXISTS folder_id UUID;
