-- Deleting a comment removes only that comment (#8296). A comment that still
-- has replies keeps its row as a tombstone — content cleared, deleted_at set —
-- so every reply stays attached to its direct parent. A comment without
-- replies is removed outright, and a tombstone is removed once its last reply
-- is gone. The application owns that lifecycle; nothing here relies on the
-- legacy parent_id cascade.
--
-- Nullable with no default, so this is a metadata-only change.
ALTER TABLE comment ADD COLUMN deleted_at TIMESTAMPTZ NULL;
