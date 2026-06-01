ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS initial_label_ids UUID[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS duplicate_guard_policy TEXT NOT NULL DEFAULT 'none'
        CHECK (duplicate_guard_policy IN ('none', 'active_run', 'active_title'));
