-- Backing index for brief_delivery's primary key, attached in 727. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_delivery_id_uidx ON brief_delivery (id);
