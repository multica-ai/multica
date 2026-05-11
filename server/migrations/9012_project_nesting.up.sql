-- Fork-specific: nested projects companion table.
-- Kept separate from project.up.sql so upstream-merges don't conflict.

CREATE TABLE IF NOT EXISTS project_nesting (
    project_id        UUID PRIMARY KEY REFERENCES project(id) ON DELETE CASCADE,
    parent_project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    show_descendants  BOOLEAN NOT NULL DEFAULT TRUE,
    depth             SMALLINT NOT NULL DEFAULT 0 CHECK (depth BETWEEN 0 AND 2),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (project_id <> parent_project_id)
);

CREATE INDEX IF NOT EXISTS idx_project_nesting_parent ON project_nesting(parent_project_id);
