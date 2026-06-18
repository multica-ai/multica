-- FIR-1423: expose more project sprint controls.

ALTER TABLE cerebro_sprint_settings
    ADD COLUMN IF NOT EXISTS move_incomplete_target_status TEXT NOT NULL DEFAULT 'todo'
        CHECK (move_incomplete_target_status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));

ALTER TABLE cerebro_sprint_recurring_task
    ADD COLUMN IF NOT EXISTS sprint_day_offset INTEGER NOT NULL DEFAULT 1
        CHECK (sprint_day_offset > 0);
