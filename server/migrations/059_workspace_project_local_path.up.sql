ALTER TABLE workspace
ADD COLUMN local_path TEXT;

ALTER TABLE project
ADD COLUMN local_path TEXT;
