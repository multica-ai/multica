-- Per-base switches for the LLM-backed WeKnora features (plan 10.11). Multica's
-- record of what it asked WeKnora to enable: the three-Wiki limit counts
-- wiki_enabled, and enable re-applies both flags to WeKnora. llm_defaults_applied
-- marks a base whose defaults were written once, so a later enable does not
-- undo a switch an admin turned off.
ALTER TABLE knowledge_base
    ADD COLUMN wiki_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN summaries_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN llm_defaults_applied BOOLEAN NOT NULL DEFAULT false;
