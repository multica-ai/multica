-- Restore unique(workspace_id, name) on squad.
ALTER TABLE squad ADD CONSTRAINT squad_workspace_id_name_key UNIQUE (workspace_id, name);
