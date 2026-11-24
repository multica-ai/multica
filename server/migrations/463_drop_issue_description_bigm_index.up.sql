-- SearchIssues evaluates description matches while scanning a workspace's
-- issue candidates, so this global description GIN is no longer read.
DROP INDEX CONCURRENTLY IF EXISTS idx_issue_description_bigm;
