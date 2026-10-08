-- Backing index for workspace_share_link's primary key, attached in 329 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
--
-- IF NOT EXISTS covers the retry where the build committed but the runner died
-- before recording the version: a bare CREATE would fail on "already exists"
-- every time, and because 329 cannot attach a primary key without this index,
-- the migrator would stay wedged there instead of moving on. The INVALID
-- leftover of a genuinely interrupted build is removed first by the cleanup
-- hook registered for this migration in cmd/migrate, so IF NOT EXISTS can never
-- skip past a half-built index.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS workspace_share_link_pkey_uidx
    ON workspace_share_link (id);
