-- When an issue's newest inbox row comes back to the inbox later than it was
-- created: the end of a snooze, or now when it is restored from the archive
-- or taken out of a snooze early. The inbox limit (inbox.ActiveLimit) ranks
-- rows by GREATEST(created_at, returned_at). Only the issue's newest row
-- carries it, so bringing back a busy issue does not outrank everything else.
-- NULL means the row ranks by created_at. Nullable with no default: a
-- catalog-only change.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS returned_at TIMESTAMPTZ;
