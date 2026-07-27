-- The column is owned by upstream migration 224. This compatibility migration
-- is intentionally irreversible so rolling it back cannot break code that
-- already relies on session continuity disclosure.
SELECT 1;
