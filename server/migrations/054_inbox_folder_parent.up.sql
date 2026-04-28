-- Subfolder support: each folder may have a parent folder belonging to the
-- same user. Top-level folders have parent_id NULL. Cycle prevention is
-- enforced in application code.

ALTER TABLE inbox_folder
    ADD COLUMN parent_id UUID REFERENCES inbox_folder(id) ON DELETE CASCADE;

CREATE INDEX idx_inbox_folder_parent ON inbox_folder(parent_id);
