ALTER TABLE cerebro_workflow_hook_policy
    ADD COLUMN IF NOT EXISTS condition_mode TEXT NOT NULL DEFAULT 'all'
        CHECK (condition_mode IN ('all', 'any'));
