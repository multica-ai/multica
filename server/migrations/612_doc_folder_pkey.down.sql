-- Dropping the constraint drops the index it took over, leaving 611's down
-- direction a no-op via IF EXISTS.
ALTER TABLE doc_folder DROP CONSTRAINT IF EXISTS doc_folder_pkey;
