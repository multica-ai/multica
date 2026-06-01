-- FIR-2627: link skill change requests back to the agent work session that
-- proposed them so the inbox notification can deep-link to the originating run.
ALTER TABLE skill_change_request
    ADD COLUMN IF NOT EXISTS work_session_id UUID
        REFERENCES work_session(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_skill_change_request_work_session
    ON skill_change_request(work_session_id)
    WHERE work_session_id IS NOT NULL;
