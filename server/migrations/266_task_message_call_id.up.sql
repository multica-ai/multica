-- Preserve the provider tool-call identifier through transcript persistence so
-- clients can match concurrent tool uses with out-of-order results.
ALTER TABLE task_message
    ADD COLUMN IF NOT EXISTS call_id TEXT;

COMMENT ON COLUMN task_message.call_id IS
    'Provider-local tool call identifier used to pair tool_use and tool_result transcript rows.';
