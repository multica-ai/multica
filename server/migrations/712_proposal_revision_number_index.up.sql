-- One row per revision of a proposal: the writers insert the next number and
-- the unique index makes a double insert fail rather than duplicate history.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS proposal_revision_proposal_number_uidx ON proposal_revision (proposal_id, revision);
