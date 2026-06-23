-- Add stage_id to issues so they can be grouped by workflow stage.
ALTER TABLE multica_issue
ADD COLUMN stage_id UUID REFERENCES multica_workflow_stage(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_stage_id ON multica_issue(stage_id);
