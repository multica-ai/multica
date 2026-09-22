CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_model_binding_base_purpose_uidx ON knowledge_model_binding (knowledge_base_id, purpose) WHERE knowledge_base_id IS NOT NULL;
