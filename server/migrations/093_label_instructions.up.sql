-- Labels can carry optional agent instructions. When an agent starts a task
-- on an issue that has labels with non-empty instructions, those instructions
-- are appended to the agent's system prompt — giving the agent label-aware
-- context without changing the assignment model.
ALTER TABLE issue_label ADD COLUMN IF NOT EXISTS instructions TEXT NOT NULL DEFAULT '';
