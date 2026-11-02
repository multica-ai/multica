-- Backing index for proposal_revision's primary key, attached in 711. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS proposal_revision_id_uidx ON proposal_revision (id);
