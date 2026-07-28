-- CEREBRO-PATCH(cerebro-reminder): FIR-3918 — drop the snooze source link. The
-- sweeper then falls back to always creating a standalone reminder row.
ALTER TABLE cerebro_reminder
    DROP COLUMN IF EXISTS source_inbox_item_id;
