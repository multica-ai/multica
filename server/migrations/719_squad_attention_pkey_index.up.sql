-- Backing index for squad_attention's primary key, attached in 720. Own
-- single-statement migration so CONCURRENTLY runs outside a transaction.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS squad_attention_squad_uidx ON squad_attention (squad_id);
