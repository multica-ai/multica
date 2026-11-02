-- Backing index for brief_feedback's primary key, attached in 732. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS brief_feedback_id_uidx ON brief_feedback (id);
