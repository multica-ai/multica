-- Allow platform-generated rows in the comment table. Used by the
-- child-done parent-notification path (MUL-2538) so the platform can post a
-- top-level comment on the parent issue without attributing it to a member
-- or agent. system rows use a zero UUID for author_id (the column is still
-- NOT NULL).
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_author_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_author_type_check
    CHECK (author_type IN ('member', 'agent', 'system'));
