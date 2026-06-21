-- FIR-1741 Model B: sessions as timeline slices.
--
-- P1 stored chapter membership as comment.chapter_id, a field that the comment
-- create path never set -> empty sessions, dead buttons. Model B removes the
-- per-comment field entirely. A session is a contiguous slice of the issue
-- timeline opened by a session-start marker; membership is DERIVED from order
-- (a timeline entry belongs to the marker that precedes it). There is no field
-- to forget to set, which is precisely the change that removes the P1 bug class.

-- 1. Drop the P1 model: the per-comment link, its index, and the chapters table.
DROP INDEX IF EXISTS idx_comment_chapter_id;

ALTER TABLE comment
    DROP COLUMN IF EXISTS chapter_id;

DROP TABLE IF EXISTS cerebro_chapters;

-- 2. The new, small session table. No comment coupling. `handoff` is a single
--    nullable JSONB blob (NULL = blank session, no carried context). Membership
--    is computed at read time from `created_at` / `position` order, never stored.
CREATE TABLE IF NOT EXISTS cerebro_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('todo', 'in_progress', 'done')),
    handoff JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);

-- No backfill: an issue with zero session rows renders as one implicit
-- "Session 1" containing the whole timeline (handled at read time). Historical
-- issues therefore look clean, with everything inside, and no rows are written.
