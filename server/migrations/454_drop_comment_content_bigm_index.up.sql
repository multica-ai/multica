-- SearchIssues now scans comments by workspace and evaluates content matches
-- during aggregation, so this global content GIN is no longer read.
DROP INDEX CONCURRENTLY IF EXISTS idx_comment_content_bigm;
