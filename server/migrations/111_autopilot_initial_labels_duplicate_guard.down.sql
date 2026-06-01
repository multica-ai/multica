ALTER TABLE autopilot
    DROP COLUMN IF EXISTS duplicate_guard_policy,
    DROP COLUMN IF EXISTS initial_label_ids;
