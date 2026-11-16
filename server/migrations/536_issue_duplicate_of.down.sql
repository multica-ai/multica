SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE issue DROP COLUMN IF EXISTS duplicate_of_issue_id;
