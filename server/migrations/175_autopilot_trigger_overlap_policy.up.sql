ALTER TABLE autopilot_trigger
    ADD COLUMN overlap_policy TEXT NOT NULL DEFAULT 'allow'
        CHECK (overlap_policy IN ('allow', 'coalesce'));

COMMENT ON COLUMN autopilot_trigger.overlap_policy IS
    'Schedule overlap policy: allow creates every occurrence; coalesce skips new work while an earlier run_only task is active.';
