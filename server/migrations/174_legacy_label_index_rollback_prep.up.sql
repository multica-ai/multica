-- Forward no-op. This migration exists only to sequence a rollback step:
-- its down removes the agent/skill rows from issue_label before 171.down
-- rebuilds the workspace-wide unique name index. Because down migrations
-- apply high->low, keeping this delete in the highest-numbered resource
-- label migration guarantees the non-issue rows are gone before both
-- 171.down (index rebuild) and 162.down (drops resource_type). There is
-- nothing to do on the way up.
SELECT 1;
