-- Backing index for brief_inquiry's primary key, attached in 716. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_inquiry_id_uidx ON brief_inquiry (id);
