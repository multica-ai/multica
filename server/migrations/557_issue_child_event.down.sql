DROP TRIGGER IF EXISTS issue_child_event_update ON issue;
DROP TRIGGER IF EXISTS issue_child_event_insert ON issue;
DROP FUNCTION IF EXISTS record_issue_child_event();
DROP TABLE IF EXISTS issue_child_event;
