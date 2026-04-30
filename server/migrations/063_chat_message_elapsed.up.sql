-- Capture per-task wall-clock duration (queue → done) on assistant chat
-- messages so the UI can render "Replied in 38s" / "Failed after 12s"
-- under each reply. BIGINT to avoid any int32 overflow concerns even
-- though chat tasks are short — keeps the column reusable for longer
-- workloads later.
ALTER TABLE chat_message ADD COLUMN elapsed_ms BIGINT;
