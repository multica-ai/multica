CREATE UNIQUE INDEX CONCURRENTLY memoryhub_binding_project_subject_uidx ON memoryhub_binding (workspace_id, scope_id, subject_type, subject_id) WHERE scope_kind = 'project' AND scope_id IS NOT NULL;
