-- Reverse FIR-1874: restore the separate status column and the (issue_id,
-- position) unique index, and drop the thread-root link. Status defaults back
-- to 'in_progress'; the prior open/closed distinction cannot be recovered from
-- resolved_at, so this is a structural restore only.
ALTER TABLE cerebro_session
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'in_progress'
        CHECK (status IN ('todo', 'in_progress', 'done'));

DROP INDEX IF EXISTS idx_cerebro_session_root_comment;
DROP INDEX IF EXISTS idx_cerebro_session_issue;

-- The forward model keyed uniqueness on root_comment_id and hardcoded
-- position = 0 for every new session row, so multiple sessions on one issue now
-- collide at position 0. Renumber position deterministically per issue (0-based,
-- ordered by created_at) so the (issue_id, position) unique index can be
-- recreated without a duplicate-key failure.
WITH ranked AS (
    SELECT id,
           ROW_NUMBER() OVER (PARTITION BY issue_id ORDER BY created_at ASC, id ASC) - 1
               AS new_position
    FROM cerebro_session
)
UPDATE cerebro_session s
SET position = ranked.new_position
FROM ranked
WHERE s.id = ranked.id
  AND s.position IS DISTINCT FROM ranked.new_position;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);

ALTER TABLE cerebro_session
    DROP COLUMN IF EXISTS root_comment_id;
