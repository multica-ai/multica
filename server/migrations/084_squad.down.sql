-- Revert assignee_type constraint.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_assignee_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_assignee_type_check
    CHECK (assignee_type IN ('member', 'agent'));

DROP TABLE IF EXISTS squad_member;
DROP TABLE IF EXISTS squad;
