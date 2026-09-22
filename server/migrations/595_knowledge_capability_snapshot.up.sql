CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_model_capability_snapshot_uidx ON knowledge_model_capability (provider_id, secret_revision, model, capability);
