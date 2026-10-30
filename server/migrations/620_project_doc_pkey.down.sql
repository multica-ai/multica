-- Dropping the constraint drops the index it took over, leaving 619's down
-- direction a no-op via IF EXISTS.
ALTER TABLE project_doc DROP CONSTRAINT IF EXISTS project_doc_pkey;
