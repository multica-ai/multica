-- Revert: restore last_task_id column, drop last_completed_at
ALTER TABLE skill_usage_process_cursor ADD COLUMN last_task_id UUID;
ALTER TABLE skill_usage_process_cursor DROP COLUMN last_completed_at;