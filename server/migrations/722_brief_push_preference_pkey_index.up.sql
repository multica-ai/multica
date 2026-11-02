-- Backing index for brief_push_preference's primary key, attached in 723. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_push_preference_id_uidx ON brief_push_preference (id);
