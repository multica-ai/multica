-- FIR-1686 — record WHEN an inbox_item was archived, so the cerebro "Archived"
-- block can default-sort by archive time (most recently archived on top)
-- instead of by the message's original created_at.
--
-- Upstream archives an item with `UPDATE inbox_item SET archived = true` from
-- many code paths (single item, by-issue, by-type, mark-all, …). Rather than
-- patching every one of those upstream statements, a BEFORE INSERT OR UPDATE
-- trigger stamps archived_at exactly when the archived flag flips on, and
-- clears it when an item is unarchived. This covers every archive path,
-- upstream or cerebro, with no upstream SQL changes.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS archived_at timestamptz;

-- Backfill existing archived rows so they get a sensible, non-null sort key.
-- created_at is the best proxy we have for items archived before this column.
UPDATE inbox_item SET archived_at = created_at WHERE archived = true AND archived_at IS NULL;

CREATE OR REPLACE FUNCTION cerebro_set_inbox_archived_at() RETURNS trigger AS $$
BEGIN
    -- Branch on TG_OP so OLD (which is null on INSERT) is only read on UPDATE.
    IF NOT NEW.archived THEN
        NEW.archived_at := NULL;
    ELSIF TG_OP = 'INSERT' THEN
        NEW.archived_at := now();
    ELSIF OLD.archived IS DISTINCT FROM NEW.archived THEN
        -- Transition false -> true on an existing row.
        NEW.archived_at := now();
    END IF;
    -- An UPDATE that leaves archived = true untouched (mark read, mute, …)
    -- falls through all branches and preserves the original archived_at.
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS cerebro_inbox_archived_at_trg ON inbox_item;
CREATE TRIGGER cerebro_inbox_archived_at_trg
    BEFORE INSERT OR UPDATE ON inbox_item
    FOR EACH ROW EXECUTE FUNCTION cerebro_set_inbox_archived_at();

-- Supports ORDER BY archived_at DESC on the archived feed.
CREATE INDEX IF NOT EXISTS cerebro_inbox_item_archived_at_idx
    ON inbox_item (recipient_type, recipient_id, archived_at DESC)
    WHERE archived = true;
