CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_model_binding_workspace_purpose_uidx ON knowledge_model_binding (workspace_id, purpose) WHERE knowledge_base_id IS NULL;
