-- Reverse FIR-1741 hardening: restore the plain (non-unique) lookup index.
DROP INDEX IF EXISTS idx_cerebro_session_issue_position;

CREATE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);
