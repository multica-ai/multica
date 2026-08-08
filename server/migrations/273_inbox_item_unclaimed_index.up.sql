-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- Lazy migration and reconcile both ask the same question: "which of this
-- person's rows have no group yet?". PARTIAL on group_id IS NULL so the index
-- covers exactly the shrinking set of unclaimed rows and disappears from the
-- planner's cost as the migration completes, rather than growing with the table
-- forever.
--
-- recipient_type is in the key because only members get groups, so the claim
-- scan filters on it before anything else.
CREATE INDEX CONCURRENTLY IF NOT EXISTS inbox_item_unclaimed_idx
    ON inbox_item (recipient_id, recipient_type, workspace_id) WHERE group_id IS NULL;
