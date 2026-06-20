DROP TRIGGER IF EXISTS cerebro_inbox_archived_at_trg ON inbox_item;
DROP FUNCTION IF EXISTS cerebro_set_inbox_archived_at();
DROP INDEX IF EXISTS cerebro_inbox_item_archived_at_idx;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS archived_at;
