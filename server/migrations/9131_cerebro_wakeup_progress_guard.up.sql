-- FIR-3098: stop empty wakeup loops after two rounds. The existing setting is
-- retained as the control, but installations still carrying the old default of
-- five move to the new safe default. An explicit 0 remains disabled.
ALTER TABLE cerebro_workspace_settings
    ALTER COLUMN wakeup_max_consecutive_loops SET DEFAULT 2;

UPDATE cerebro_workspace_settings
SET wakeup_max_consecutive_loops = 2,
    updated_at = now()
WHERE wakeup_max_consecutive_loops = 5;
