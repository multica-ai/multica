-- issue.assignee_type allows member/agent/squad, but the multi-assignee
-- table shipped with only member/agent — any CreateIssue with a squad
-- primary assignee 500s on the "add primary assignee" insert. Widen the
-- check to match the issue table.
ALTER TABLE issue_assignees DROP CONSTRAINT issue_assignees_assignee_type_check;
ALTER TABLE issue_assignees ADD CONSTRAINT issue_assignees_assignee_type_check
  CHECK (assignee_type IN ('member', 'agent', 'squad'));
