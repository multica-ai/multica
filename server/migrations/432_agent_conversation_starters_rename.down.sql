-- Reverse of 432. Rename the constraint back first so the column rename lands
-- on the same names migration 404 created.
ALTER TABLE agent
    RENAME CONSTRAINT agent_conversation_starters_check
    TO agent_starter_prompts_check;

ALTER TABLE agent
    RENAME COLUMN conversation_starters TO starter_prompts;
