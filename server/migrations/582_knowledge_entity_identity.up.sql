CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_entity_identity_uidx ON knowledge_entity (knowledge_base_id, identity_key);
