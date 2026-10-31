-- Dropping the table erases every tombstone: identifiers whose issues were
-- deleted go back to the same not-found as never-issued numbers (#9070).
-- Nothing else changes — the table is write-only outside the narrow tombstone
-- lookup, so no other behavior depends on it.
DROP TABLE IF EXISTS deleted_issue;
