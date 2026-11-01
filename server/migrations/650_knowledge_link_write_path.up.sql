-- What a write needs to be listed, retried and published later.
--
-- title: shown in the Drafts list without asking WeKnora.
-- source_url: a URL import is held here as a draft; WeKnora fetches it only
--   at publish, so weknora_knowledge_id stays '' until then.
-- pending_content: the Markdown of a write WeKnora has not accepted yet (the
--   row is in state 'failed'); cleared once it has. A retry reads it back.
-- publish_requested: whether the write should be indexed at once when it is
--   retried (system writers and member "save to knowledge").
-- error: the last failure, for the failed row.
ALTER TABLE knowledge_link
    ADD COLUMN title TEXT NOT NULL DEFAULT '',
    ADD COLUMN source_url TEXT NOT NULL DEFAULT '',
    ADD COLUMN pending_content TEXT NOT NULL DEFAULT '',
    ADD COLUMN publish_requested BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN error TEXT NOT NULL DEFAULT '';
