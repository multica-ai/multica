-- CEREBRO-PATCH(cerebro-reminder): FIR-3918 — remember which inbox row a
-- reminder was snoozed from.
--
-- Snoozing an inbox row ("Remind me") writes TWO things: it mutes the row until
-- the chosen time (it resurfaces by itself when muted_until passes) and it
-- creates a cerebro_reminder. At fire time the sweeper then dropped a second,
-- standalone `reminder` inbox row (FIR-2278) — so one message came back as two
-- rows with the same title.
--
-- The reminder had no way to know it came from a row that resurfaces on its own.
-- source_inbox_item_id is that link: when it points at a live row, the sweeper
-- lets that row BE the reminder instead of creating a copy. NULL for every other
-- reminder kind (free, project, comment, chat), which keeps their standalone row.
--
-- ON DELETE SET NULL matches the other anchor columns: losing the source row
-- leaves the reminder as a safe dangling row instead of cascading it away.
ALTER TABLE cerebro_reminder
    ADD COLUMN IF NOT EXISTS source_inbox_item_id UUID
        REFERENCES inbox_item(id) ON DELETE SET NULL;
