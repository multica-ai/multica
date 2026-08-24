-- Skill usage processor: switch cursor from UUID (last_task_id) to timestamp
-- (last_completed_at). UUID v4 is random — "WHERE id > $1 ORDER BY id" skips
-- tasks whose UUID sorts before the cursor, even though they were created
-- later. Timestamp ordering guarantees every completed task is eventually
-- processed.

ALTER TABLE skill_usage_process_cursor ADD COLUMN last_completed_at TIMESTAMPTZ;
UPDATE skill_usage_process_cursor SET last_completed_at = NULL;
ALTER TABLE skill_usage_process_cursor DROP COLUMN last_task_id;