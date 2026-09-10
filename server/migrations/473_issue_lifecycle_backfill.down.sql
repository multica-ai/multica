UPDATE issue
SET last_transition_id = NULL,
    lifecycle_status_id = NULL,
    lifecycle_id = NULL;

UPDATE project SET default_issue_lifecycle_id = NULL;
UPDATE workspace SET default_issue_lifecycle_id = NULL;

DELETE FROM automation_execution;
DELETE FROM issue_transition;
DELETE FROM issue_lifecycle_status;
DELETE FROM issue_lifecycle;
