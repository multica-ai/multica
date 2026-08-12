-- Single-statement CONCURRENTLY migration: Postgres rejects CREATE INDEX
-- CONCURRENTLY inside a transaction or a multi-command string, and the
-- migration runner execs each file as one command (see 080/135).
--
-- Enforces the monotonic-uniqueness invariant of domain_event.seq and serves
-- the ordered `... ORDER BY seq` drain/scan the PR3 matcher will use.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_domain_event_seq
    ON domain_event (seq);
