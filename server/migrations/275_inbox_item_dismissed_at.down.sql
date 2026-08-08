-- Dropping the column loses the distinction and returns to "dismissed looks
-- like archived", which is the pre-migration behaviour.
ALTER TABLE inbox_item DROP COLUMN IF EXISTS dismissed_at;
