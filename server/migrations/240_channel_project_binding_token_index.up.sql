CREATE UNIQUE INDEX CONCURRENTLY uq_channel_project_binding_bind_token ON channel_project_binding (bind_token_hash) WHERE state = 'pending_group' AND bind_token_hash IS NOT NULL;
