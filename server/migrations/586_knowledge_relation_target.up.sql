CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_relation_target_idx ON knowledge_relation (knowledge_base_id, target_entity_id);
