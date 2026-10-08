-- Dropping the constraint drops the index it took over, leaving 333's down
-- direction a no-op via IF EXISTS.
ALTER TABLE issue_status DROP CONSTRAINT IF EXISTS issue_status_pkey;
