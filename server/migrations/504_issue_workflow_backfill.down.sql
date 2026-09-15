UPDATE issue
SET last_transition_id = NULL,
    workflow_status_id = NULL,
    workflow_id = NULL;

UPDATE project SET default_issue_workflow_id = NULL;
UPDATE workspace SET default_issue_workflow_id = NULL;

DELETE FROM automation_execution;
DELETE FROM issue_transition;
DELETE FROM issue_workflow_status;
DELETE FROM issue_workflow;
