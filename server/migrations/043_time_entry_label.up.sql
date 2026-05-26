-- Workspace-scoped labels for time entries.
CREATE TABLE IF NOT EXISTS time_entry_label (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    color TEXT NOT NULL DEFAULT '#6b7280',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Many-to-many relation between time entries and labels.
CREATE TABLE IF NOT EXISTS time_entry_to_label (
    time_entry_id UUID NOT NULL REFERENCES time_entry(id) ON DELETE CASCADE,
    label_id UUID NOT NULL REFERENCES time_entry_label(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (time_entry_id, label_id)
);

CREATE UNIQUE INDEX IF NOT EXISTS time_entry_label_workspace_name_unique ON time_entry_label (workspace_id, name);
CREATE INDEX IF NOT EXISTS idx_time_entry_label_workspace ON time_entry_label (workspace_id);
CREATE INDEX IF NOT EXISTS idx_time_entry_to_label_label ON time_entry_to_label (label_id);
