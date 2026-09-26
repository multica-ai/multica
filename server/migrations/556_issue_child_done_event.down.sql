DROP TRIGGER IF EXISTS issue_child_done_event_trigger ON issue;
DROP FUNCTION IF EXISTS record_issue_child_done();
DROP TABLE IF EXISTS issue_child_done_event;
