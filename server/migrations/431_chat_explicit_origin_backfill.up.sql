-- The idempotent pre-migration hook performs the legacy Chat origin backfill
-- in short primary-key pages before this marker is recorded.
SELECT 1;
