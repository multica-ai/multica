-- Backing index for workspace_share_link's primary key, attached in 316 via
-- PRIMARY KEY USING INDEX. Own single-statement migration so CONCURRENTLY runs
-- outside an implicit transaction (repo convention).
CREATE UNIQUE INDEX CONCURRENTLY workspace_share_link_pkey_uidx
    ON workspace_share_link (id);
