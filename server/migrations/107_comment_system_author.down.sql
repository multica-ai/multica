-- Drop any platform-generated rows before re-tightening the CHECK so the
-- ADD CONSTRAINT does not fail on existing system comments.
DELETE FROM comment WHERE author_type = 'system';
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_author_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_author_type_check
    CHECK (author_type IN ('member', 'agent'));
