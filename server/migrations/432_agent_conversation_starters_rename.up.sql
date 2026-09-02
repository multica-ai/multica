-- Rename the agent starter-prompts column to the product name it ships under.
-- The feature is unreleased (the column arrived in migration 404, after the
-- last release tag), so this is a pure rename with no compatibility window:
-- no deployed client reads the old name. Both statements are sent in one
-- migration query and therefore commit atomically.
ALTER TABLE agent
    RENAME COLUMN starter_prompts TO conversation_starters;

ALTER TABLE agent
    RENAME CONSTRAINT agent_starter_prompts_check
    TO agent_conversation_starters_check;
