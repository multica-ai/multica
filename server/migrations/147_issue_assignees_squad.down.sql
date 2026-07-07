ALTER TABLE issue_assignees DROP CONSTRAINT issue_assignees_assignee_type_check;
ALTER TABLE issue_assignees ADD CONSTRAINT issue_assignees_assignee_type_check
  CHECK (assignee_type IN ('member', 'agent'));
