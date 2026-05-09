-- Remove the triggers and tools columns from agent table.
-- Trigger behavior (on_assign, on_comment, on_mention) is now always enabled (hardcoded).
-- Tools was a placeholder field never used at runtime.
ALTER TABLE agent DROP COLUMN IF EXISTS triggers;
ALTER TABLE agent DROP COLUMN IF EXISTS tools;
