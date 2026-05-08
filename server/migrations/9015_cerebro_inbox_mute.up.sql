-- Cerebro-only: per-item mute support for inbox.
--
-- Muting an inbox item silences notifications + badges for that item until
-- the timestamp passes. Default mute duration is "until tomorrow 08:00 in the
-- user's local timezone" (computed client-side, sent as RFC3339).
--
-- The item still appears in the inbox listing (mute is not archive); only
-- notification side-effects and the unread badge count are suppressed.
ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS muted_until TIMESTAMPTZ;

-- Index for the unread-count queries that filter by route + recipient and
-- now also need to skip muted rows. NULL muted_until means "not muted",
-- which postgres places at the end with default ordering — using a partial
-- index keeps it small (only muted rows).
CREATE INDEX IF NOT EXISTS idx_inbox_muted_until
    ON inbox_item(muted_until)
    WHERE muted_until IS NOT NULL;
