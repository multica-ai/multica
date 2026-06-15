-- Wave 3 / G3 (TECH-3556) — interim edit lock on a Note.
--
-- Full live co-editing (multiple cursors, CRDT) is a separate, larger project.
-- Until then two people opening the same note can silently overwrite each
-- other. This adds a soft, single-writer lock directly on cerebro_note:
--
--   locked_by         — the member currently holding the edit lock (NULL = free)
--   locked_at         — when the lock was first acquired (for "editing since")
--   lock_heartbeat_at — last heartbeat from the holder's editor
--
--   LOCK MODEL — heartbeat + timeout (no server-side timers needed).
--   The editor sends a heartbeat (~every 30s) while a note is open. A lock is
--   considered LIVE only while lock_heartbeat_at is within a timeout window
--   (a handler constant, ~90s). A holder who closes the tab or crashes simply
--   stops heartbeating; the lock goes stale and the next editor can take it
--   without any sweeper. This is computed at read time (now() - heartbeat), so
--   a forgotten lock never wedges a note. A second editor seeing a LIVE lock
--   held by someone else is shown read-only with "X is editing" and a
--   "Take over" action that force-acquires (steals) the lock.
--
-- All three columns are nullable and default NULL, so existing notes are
-- unlocked. No new table — the lock is per-note state that belongs on the note.
ALTER TABLE cerebro_note
    ADD COLUMN IF NOT EXISTS locked_by         UUID,
    ADD COLUMN IF NOT EXISTS locked_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS lock_heartbeat_at TIMESTAMPTZ;
