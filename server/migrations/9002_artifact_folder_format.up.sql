-- Folders for artifacts. One tree per workspace, recursive via parent_id.
-- Folders are orthogonal to scope: an artifact in a folder may still be
-- attached to a project or issue; project/issue are alternative groupings.

CREATE TABLE artifact_folder (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    parent_id    UUID REFERENCES artifact_folder(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, parent_id, name)
);

CREATE INDEX idx_artifact_folder_workspace ON artifact_folder(workspace_id);
CREATE INDEX idx_artifact_folder_parent ON artifact_folder(parent_id) WHERE parent_id IS NOT NULL;

-- folder_id: null = root of workspace tree.
-- format: how the artifact body/file is rendered. 'md' and 'html' use body
-- (text), 'pdf' uses file_url + file_size_bytes pointing at storage.
ALTER TABLE artifact ADD COLUMN folder_id UUID REFERENCES artifact_folder(id) ON DELETE SET NULL;
ALTER TABLE artifact ADD COLUMN format TEXT NOT NULL DEFAULT 'md' CHECK (format IN ('md', 'html', 'pdf'));
ALTER TABLE artifact ADD COLUMN file_url TEXT;
ALTER TABLE artifact ADD COLUMN file_size_bytes BIGINT;

CREATE INDEX idx_artifact_folder ON artifact(folder_id) WHERE folder_id IS NOT NULL;
