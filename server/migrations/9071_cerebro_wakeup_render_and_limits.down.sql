-- Revert TECH-3298 wakeup notes + self-wakeup limits.

ALTER TABLE cerebro_workspace_settings
    DROP COLUMN IF EXISTS wakeup_min_interval_minutes,
    DROP COLUMN IF EXISTS wakeup_max_self_per_issue;

-- Any wakeup-typed rows must be demoted before the old CHECK can be restored.
UPDATE comment SET type = 'system' WHERE type = 'wakeup';
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system'));
