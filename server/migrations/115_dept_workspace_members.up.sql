ALTER TABLE multica_user
    ADD COLUMN IF NOT EXISTS casdoor_universal_id TEXT UNIQUE;

ALTER TABLE multica_member
    ALTER COLUMN user_id DROP NOT NULL,
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS external_user_id TEXT,
    ADD COLUMN IF NOT EXISTS external_universal_id TEXT,
    ADD COLUMN IF NOT EXISTS employee_id TEXT,
    ADD COLUMN IF NOT EXISTS org_display_name TEXT,
    ADD COLUMN IF NOT EXISTS dept_id TEXT,
    ADD COLUMN IF NOT EXISTS dept_name TEXT,
    ADD COLUMN IF NOT EXISTS dept_path TEXT,
    ADD COLUMN IF NOT EXISTS position TEXT,
    ADD COLUMN IF NOT EXISTS is_main_department BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS dept_user_status INTEGER,
    ADD COLUMN IF NOT EXISTS last_synced_at TIMESTAMPTZ;

ALTER TABLE multica_member
    ADD CONSTRAINT multica_member_source_check
        CHECK (source IN ('manual', 'dept')),
    ADD CONSTRAINT multica_member_status_check
        CHECK (status IN ('active', 'pending_activation', 'inactive'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_multica_member_workspace_external_universal
    ON multica_member(workspace_id, external_universal_id)
    WHERE external_universal_id IS NOT NULL AND external_universal_id <> '';

CREATE INDEX IF NOT EXISTS idx_multica_member_workspace_status
    ON multica_member(workspace_id, status);

CREATE INDEX IF NOT EXISTS idx_multica_user_casdoor_universal_id
    ON multica_user(casdoor_universal_id);
