-- Fork-specific: per-item routing decision for inbox items.
-- Items with route='inbox' show up in the inbox page (existing behavior).
-- Items with route='notifications' show up in a separate, less-intrusive
-- notifications page anchored in the bottom of the sidebar.
-- The route is decided at insert time from the recipient's preferences;
-- changing preferences later does NOT retroactively shuffle existing items.

ALTER TABLE inbox_item
    ADD COLUMN IF NOT EXISTS route TEXT NOT NULL DEFAULT 'inbox'
    CHECK (route IN ('inbox', 'notifications'));

-- Existing index covers (recipient_type, recipient_id, read). Add a route-aware
-- index so list/count queries can filter cheaply.
CREATE INDEX IF NOT EXISTS idx_inbox_recipient_route
    ON inbox_item(recipient_type, recipient_id, route, archived);
