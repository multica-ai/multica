CREATE UNIQUE INDEX CONCURRENTLY memoryhub_binding_workspace_subject_uidx ON memoryhub_binding (workspace_id, subject_type, subject_id) WHERE scope_kind = 'workspace' AND scope_id IS NULL;
