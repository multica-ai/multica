-- Note: this fails if any comment already has author_type = 'plugin'. That is
-- the correct outcome — rolling back the constraint would otherwise leave rows
-- the schema says cannot exist. Delete or re-attribute those rows first.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_author_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_author_type_check
    CHECK (author_type IN ('member', 'agent', 'system'));
ALTER TABLE plugin_installation DROP COLUMN IF EXISTS token_rotated_at;
ALTER TABLE plugin_installation DROP COLUMN IF EXISTS token_hash;
DROP TABLE IF EXISTS plugin_invocation;
