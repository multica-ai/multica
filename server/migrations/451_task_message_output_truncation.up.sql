-- Records whether task_message.output is the whole tool result or a preview.
--
-- Nullable with no default, and that is the point: three states have to be
-- distinguishable. NULL means the reporting daemon predates these columns and
-- cannot say — every row written before this migration is in that state, and
-- no backfill can fix it because the original size was never recorded
-- anywhere. false is a positive assertion that the output is complete; true
-- means it was truncated to a preview.
--
-- Defaulting to false would erase that distinction and assert completeness for
-- every historical row, which is the silent-truncation bug this work removes.
ALTER TABLE task_message ADD COLUMN IF NOT EXISTS output_truncated BOOLEAN;

-- Size of the tool result before truncation, in bytes. BIGINT because a tool
-- can emit more than 2 GB and the count must not wrap.
--
-- Holds a length only, never content, so it neither widens what a transcript
-- reader can see nor bypasses server-side redaction.
ALTER TABLE task_message ADD COLUMN IF NOT EXISTS output_original_bytes BIGINT;
