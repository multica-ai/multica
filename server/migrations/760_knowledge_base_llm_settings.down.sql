ALTER TABLE knowledge_base
    DROP COLUMN IF EXISTS llm_defaults_applied,
    DROP COLUMN IF EXISTS summaries_enabled,
    DROP COLUMN IF EXISTS wiki_enabled;
