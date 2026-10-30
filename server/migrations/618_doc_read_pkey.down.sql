-- Dropping the constraint drops the index it took over, leaving 617's down
-- direction a no-op via IF EXISTS.
ALTER TABLE doc_read DROP CONSTRAINT IF EXISTS doc_read_pkey;
