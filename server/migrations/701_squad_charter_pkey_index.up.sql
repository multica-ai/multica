-- Backing index for squad_charter's primary key, attached in 703 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside a transaction (repo convention, see 632).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_charter_pkey_uidx ON squad_charter (squad_id);
