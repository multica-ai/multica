-- Retire the portable fallback for the same workspace-first SearchIssues path.
-- Keep this separate because concurrent index DDL must be the only statement.
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_content_trgm;
