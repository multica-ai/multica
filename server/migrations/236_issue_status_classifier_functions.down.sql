-- Drop the issue-status classifier functions added in migration 236.  The
-- queries that reference them are rewritten back to inline literals by the
-- corresponding code revert; dropping the functions is safe once nothing
-- references them.
DROP FUNCTION IF EXISTS issue_status_is_completed(text);
DROP FUNCTION IF EXISTS issue_status_is_closed(text);
