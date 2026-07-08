-- Index on "user".created_at to support ordering/filtering users by signup time.
--
-- "user" is a reserved word so it stays double-quoted. The migration runner
-- executes files outside an explicit transaction, so CONCURRENTLY is kept in
-- its own single-statement migration to avoid Postgres' implicit transaction
-- block for multi-statement query strings.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_created_at
    ON "user" (created_at);
