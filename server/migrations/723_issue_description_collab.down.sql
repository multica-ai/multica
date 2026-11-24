DROP TABLE IF EXISTS issue_description_revision;
DROP TABLE IF EXISTS issue_description_collab_state;
ALTER TABLE issue DROP COLUMN IF EXISTS description_revision;
