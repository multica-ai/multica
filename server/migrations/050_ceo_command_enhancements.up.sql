-- Add is_ceo_command flag to workflow so CEO-created ad-hoc workflows
-- can be separated from user-defined workflows.
ALTER TABLE workflow ADD COLUMN is_ceo_command BOOLEAN NOT NULL DEFAULT FALSE;

-- Add skip_review flag to workflow_run so CEO commands can opt out of
-- review steps during execution.
ALTER TABLE workflow_run ADD COLUMN skip_review BOOLEAN NOT NULL DEFAULT FALSE;
