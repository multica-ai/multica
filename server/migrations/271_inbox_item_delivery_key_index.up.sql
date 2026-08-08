-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file.
--
-- Idempotency for new deliveries, scoped to the recipient.
--
-- The key is (workspace_id, recipient_id, delivery_key), not delivery_key
-- alone. A bare global key makes correctness depend on every producer
-- remembering to fold the workspace and the recipient into the string it
-- builds: one that forgets — "issue:<id>:comment:<id>" is the obvious shape to
-- reach for — silently collides across tenants, and the SECOND recipient's
-- notification is not written at all. Scoping the constraint to the owner means
-- a producer can only ever deduplicate a person against themselves, which is
-- the only thing delivery keys are for.
--
-- PARTIAL on delivery_key IS NOT NULL because every pre-existing row has NULL
-- there: a plain unique index would be satisfied by them (Postgres treats NULLs
-- as distinct) but would also index the entire history for nothing.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS inbox_item_delivery_key_uidx
    ON inbox_item (workspace_id, recipient_id, delivery_key) WHERE delivery_key IS NOT NULL;
