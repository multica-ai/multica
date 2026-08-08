-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file.
--
-- Two jobs at once. It enforces that a sequence number is used once per group —
-- the property the read cursor depends on — and it is the index the group's
-- representative-row lookup and its downward recomputation both scan, which is
-- why event_seq is DESC.
--
-- PARTIAL on group_id IS NOT NULL: unclaimed rows have no group and no sequence,
-- and indexing them would be indexing the whole pre-migration history to
-- enforce a constraint that cannot apply to it.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS inbox_item_group_seq_uidx
    ON inbox_item (group_id, event_seq DESC) WHERE group_id IS NOT NULL;
