-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file.
--
-- The group's identity, and the ON CONFLICT target the delivery upsert needs.
-- Without it a concurrent delivery and a lazy migration could each create a
-- group for the same (person, source) and the counts would drift exactly the
-- way they do today.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS inbox_group_identity_uidx
    ON inbox_group (workspace_id, recipient_id, source_kind, source_id);
