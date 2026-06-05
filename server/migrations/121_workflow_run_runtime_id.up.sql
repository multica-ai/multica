ALTER TABLE multica_workflow_run ADD COLUMN runtime_id UUID REFERENCES multica_agent_runtime(id);
