CREATE TABLE IF NOT EXISTS cerebro_chapters (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('todo', 'in_progress', 'done')),
    position INT NOT NULL DEFAULT 0,
    handoff_summary TEXT NOT NULL DEFAULT '',
    handoff_done JSONB NOT NULL DEFAULT '[]'::jsonb,
    handoff_remaining JSONB NOT NULL DEFAULT '[]'::jsonb,
    plan_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_chapters_issue_position
    ON cerebro_chapters(issue_id, position);

ALTER TABLE comment
    ADD COLUMN IF NOT EXISTS chapter_id UUID REFERENCES cerebro_chapters(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_comment_chapter_id
    ON comment(chapter_id)
    WHERE chapter_id IS NOT NULL;

-- Backfill: every issue that already has root comments gets one default
-- "Session 1" chapter, and its root comments are assigned to it. Replies
-- (parent_id IS NOT NULL) keep chapter_id NULL and render under their root
-- thread's chapter. Existing issues therefore render unchanged.
WITH issue_roots AS (
    SELECT issue_id, MIN(created_at) AS first_comment_at
    FROM comment
    WHERE parent_id IS NULL
    GROUP BY issue_id
),
inserted AS (
    INSERT INTO cerebro_chapters (issue_id, name, status, position, created_at, updated_at)
    SELECT issue_id, 'Session 1', 'in_progress', 1, first_comment_at, now()
    FROM issue_roots
    RETURNING id, issue_id
)
UPDATE comment c
SET chapter_id = inserted.id
FROM inserted
WHERE c.issue_id = inserted.issue_id
  AND c.parent_id IS NULL
  AND c.chapter_id IS NULL;
