CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_relation_identity_uidx ON knowledge_relation (knowledge_base_id, identity_key);
