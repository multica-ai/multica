-- Reverse FIR-1874: restore the separate status column and the (issue_id,
-- position) unique index, and drop the thread-root link. Status defaults back
-- to 'in_progress'; the prior open/closed distinction cannot be recovered from
-- resolved_at, so this is a structural restore only.
ALTER TABLE cerebro_session
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'in_progress'
        CHECK (status IN ('todo', 'in_progress', 'done'));

DROP INDEX IF EXISTS idx_cerebro_session_root_comment;
DROP INDEX IF EXISTS idx_cerebro_session_issue;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);

ALTER TABLE cerebro_session
    DROP COLUMN IF EXISTS root_comment_id;
