-- Reverse FIR-1741: drop the session table and restore the P1 chapters model
-- (mirrors 9096_cerebro_chapters.up.sql) so migrate-down returns to that state.
DROP TABLE IF EXISTS cerebro_session;

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
