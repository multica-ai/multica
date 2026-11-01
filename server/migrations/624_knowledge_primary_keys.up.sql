-- Attach the CONCURRENTLY-built unique indexes (621-623) as primary keys.
ALTER TABLE workspace_knowledge
    ADD CONSTRAINT workspace_knowledge_pkey PRIMARY KEY USING INDEX workspace_knowledge_pkey_uidx;
ALTER TABLE knowledge_base
    ADD CONSTRAINT knowledge_base_pkey PRIMARY KEY USING INDEX knowledge_base_pkey_uidx;
ALTER TABLE knowledge_link
    ADD CONSTRAINT knowledge_link_pkey PRIMARY KEY USING INDEX knowledge_link_pkey_uidx;
