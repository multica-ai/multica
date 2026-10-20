-- Issue numbers are no longer unique per workspace: a project-scoped issue and a
-- workspace-scoped issue may both be number 1 (PROA-1 / SHAR-1). Uniqueness now
-- holds per numbering scope and is maintained by the application (see
-- service.AllocateIssueNumber and the project prefix renumbering paths).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS uq_issue_workspace_number;
