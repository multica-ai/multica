CREATE UNIQUE INDEX CONCURRENTLY uq_channel_project_binding_project ON channel_project_binding (project_id) WHERE state IN ('pending_group', 'active');
