-- Add work_dir to workflow_run so all steps in a workflow run share the same
-- execution directory, avoiding cold-start directory creation for each step.
ALTER TABLE workflow_run ADD COLUMN work_dir TEXT;
