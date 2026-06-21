-- FIR-1741 hardening (Codex review of PR #1657): prevent duplicate session
-- positions on the same issue. Two concurrent `Start fresh` calls could each
-- read the same MAX(position)+1 and insert it, producing duplicate "Session N"
-- rows / multiple open sessions — the P1 failure class (empty/duplicate
-- sessions) through a new door. The StartFresh handler now takes a per-issue
-- transaction advisory lock; this unique index is the database-level backstop
-- and replaces the plain (issue_id, position) lookup index (a unique index
-- serves the same lookups).
DROP INDEX IF EXISTS idx_cerebro_session_issue_position;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_session_issue_position
    ON cerebro_session(issue_id, position);
