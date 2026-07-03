-- Thread the per-tool call id and failure bit through the task_message wire
-- boundary so the chat timeline can pair tool_use ↔ tool_result by id (instead
-- of positionally) and render a real running/done/error status. Both columns
-- are nullable with no backfill: rows written before this migration return
-- NULL and the frontend falls back to positional pairing for them. See MUL-27.
ALTER TABLE task_message ADD COLUMN call_id TEXT;
ALTER TABLE task_message ADD COLUMN is_error BOOLEAN;
