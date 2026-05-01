DROP INDEX IF EXISTS idx_inbox_recipient_route;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS route;
