-- Dropping the constraint drops the index it took over, leaving 615's down
-- direction a no-op via IF EXISTS.
ALTER TABLE doc_version DROP CONSTRAINT IF EXISTS doc_version_pkey;
