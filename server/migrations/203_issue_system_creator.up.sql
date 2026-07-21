-- Allow platform-generated rows in the issue table, mirroring what
-- 107_comment_system_author did for comment.author_type. Used by the
-- onboarding no-runtime seed endpoint (MUL-5118) so the starter issues a
-- fresh workspace receives are attributed to the platform ("Multica" in the
-- UI) instead of the member who happened to complete onboarding. system rows
-- use a zero UUID for creator_id (the column is still NOT NULL).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_creator_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_creator_type_check
    CHECK (creator_type IN ('member', 'agent', 'system'));
