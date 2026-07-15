-- FIR-3172: real nested catalog folders replace the legacy free-text label.
CREATE TABLE IF NOT EXISTS cerebro_app_folder (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES cerebro_app_folder(id) ON DELETE SET NULL,
    name TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_app_folder_sibling_name
    ON cerebro_app_folder(workspace_id, COALESCE(parent_id, '00000000-0000-0000-0000-000000000000'::uuid), lower(name));
ALTER TABLE cerebro_app ADD COLUMN IF NOT EXISTS folder_id UUID REFERENCES cerebro_app_folder(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_cerebro_app_folder_id ON cerebro_app(folder_id);

