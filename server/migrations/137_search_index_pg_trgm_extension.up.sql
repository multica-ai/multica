-- Install pg_trgm for the fallback trigram search indexes added in the next
-- migrations. Keep this separate from CREATE INDEX CONCURRENTLY files:
-- Postgres rejects concurrent index builds in a multi-statement migration.
CREATE EXTENSION IF NOT EXISTS pg_trgm;
