-- Dropping the constraint drops the index it took over, leaving 613's down
-- direction a no-op via IF EXISTS.
ALTER TABLE doc DROP CONSTRAINT IF EXISTS doc_pkey;
