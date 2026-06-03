DROP INDEX IF EXISTS idx_skill_change_request_work_session;
ALTER TABLE skill_change_request DROP COLUMN IF EXISTS work_session_id;
