-- FIR-1874: thread = session. Jesper's final decision (B-100%): a session IS a
-- comment thread, and a session's open/closed state IS the thread root's
-- resolved_at. We therefore drop the two pieces of "double logic" the previous
-- model carried — the separate session `status` column and the time-window
-- membership — and key each session row to its thread root via root_comment_id.
--
-- A cerebro_session row is now OPTIONAL metadata (name/handoff) hung on a thread
-- root; membership is the thread itself (root + replies), and state comes from
-- comment.resolved_at. No markers, no status, no time windows.

-- 1. Tie a session to its thread root. ON DELETE CASCADE so removing a thread
--    removes its session metadata.
ALTER TABLE cerebro_session
    ADD COLUMN IF NOT EXISTS root_comment_id UUID REFERENCES comment(id) ON DELETE CASCADE;

-- 2. Best-effort backfill: the old model opened a session marker at (or just
--    before) each new thread's root comment, so map every existing session row
--    to the root comment whose created_at is closest at/after the marker. Rows
--    that cannot be mapped keep root_comment_id NULL and stop rendering (the
--    feature is days old and flag-gated, so the data loss is negligible).
UPDATE cerebro_session s
SET root_comment_id = (
    SELECT c.id
    FROM comment c
    WHERE c.issue_id = s.issue_id
      AND c.parent_id IS NULL
      AND c.type = 'comment'
      AND c.created_at >= s.created_at - INTERVAL '2 seconds'
    ORDER BY c.created_at ASC, c.id ASC
    LIMIT 1
)
WHERE s.root_comment_id IS NULL;

-- 3. One session row per thread root. Replaces the (issue_id, position) unique
--    index — position no longer drives membership or ordering (the thread root's
--    created_at does).
DROP INDEX IF EXISTS idx_cerebro_session_issue_position;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_session_root_comment
    ON cerebro_session(root_comment_id)
    WHERE root_comment_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_cerebro_session_issue
    ON cerebro_session(issue_id);

-- 4. Drop the separate status — open/closed is the thread root's resolved_at now.
ALTER TABLE cerebro_session
    DROP COLUMN IF EXISTS status;
