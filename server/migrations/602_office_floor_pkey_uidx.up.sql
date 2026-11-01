-- Backing index for office_floor's primary key, attached in 603 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS office_floor_pkey_uidx ON office_floor (id);
