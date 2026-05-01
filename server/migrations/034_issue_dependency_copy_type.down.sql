DELETE FROM issue_dependency
WHERE type = 'copy';

ALTER TABLE issue_dependency
DROP CONSTRAINT IF EXISTS issue_dependency_type_check;

ALTER TABLE issue_dependency
ADD CONSTRAINT issue_dependency_type_check
CHECK (type IN ('blocks', 'blocked_by', 'related'));