CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_entity_merge_target_idx ON knowledge_entity (knowledge_base_id, merged_into_id) WHERE merged_into_id IS NOT NULL;
