-- Backing index for issue_status's primary key, attached in 334 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY issue_status_pkey_uidx
    ON issue_status (id);
