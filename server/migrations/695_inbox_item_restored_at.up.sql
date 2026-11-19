-- When an archived notification was last restored to the inbox. Restoring
-- counts as the row coming back, like a snooze running out, so the inbox limit
-- (inbox.ActiveLimit) ranks it by this time rather than by created_at. NULL
-- means never restored. Nullable with no default: a catalog-only change.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS restored_at TIMESTAMPTZ;
