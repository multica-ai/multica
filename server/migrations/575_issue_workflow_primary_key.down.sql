-- Dropping the constraint drops the index it took over, leaving 574's down
-- direction a no-op via IF EXISTS.
ALTER TABLE issue_workflow DROP CONSTRAINT IF EXISTS issue_workflow_pkey;
