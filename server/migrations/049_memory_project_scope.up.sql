ALTER TABLE workspace_memory
  ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE CASCADE;
CREATE INDEX idx_workspace_memory_project_id ON workspace_memory(project_id);
