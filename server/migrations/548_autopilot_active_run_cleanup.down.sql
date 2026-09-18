-- Historical duplicate cleanup is intentionally irreversible. The failed rows
-- and their quota settlement are retained as audit evidence on rollback.
SELECT 1;
