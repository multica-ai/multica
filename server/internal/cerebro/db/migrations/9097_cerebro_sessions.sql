-- FIR-1741 Model B sqlc schema input. Mirrors server/migrations/9097_cerebro_sessions.up.sql.
-- Drops the P1 chapters model and defines the session table so sqlc generates
-- type-safe Go against cerebro_session (and never against the removed columns).
DROP INDEX IF EXISTS idx_comment_chapter_id;

ALTER TABLE comment
    DROP COLUMN IF EXISTS chapter_id;

DROP TABLE IF EXISTS cerebro_chapters;

CREATE TABLE IF NOT EXISTS cerebro_session (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    position INT NOT NULL DEFAULT 0,
    name TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'auto' CHECK (mode IN ('auto', 'plan', 'build', 'research', 'review')),
    status TEXT NOT NULL DEFAULT 'in_progress' CHECK (status IN ('todo', 'in_progress', 'done')),
    handoff JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);
