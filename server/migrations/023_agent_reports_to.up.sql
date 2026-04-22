ALTER TABLE agent ADD COLUMN reports_to UUID REFERENCES agent(id) ON DELETE SET NULL;
CREATE INDEX idx_agent_reports_to ON agent(workspace_id, reports_to);
