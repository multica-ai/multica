ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_parent_id_fkey;
ALTER TABLE comment ADD CONSTRAINT comment_parent_id_fkey
    FOREIGN KEY (parent_id) REFERENCES comment(id) ON DELETE CASCADE;
