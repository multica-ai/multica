-- Unique lookup index for the exchange path (MUL-5112): the redeem query
-- resolves a token by its SHA-256 hash, and uniqueness guards against the
-- astronomically-unlikely hash collision. Separate single-statement migration
-- because PostgreSQL rejects CREATE INDEX CONCURRENTLY inside a transaction or
-- a multi-command string.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_setup_token_hash
    ON setup_token (token_hash);
