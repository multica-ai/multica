-- Closing an issue disables its wakeups and cancels unstarted wakeup runs in
-- the application transaction that writes the status
-- (service.StopClosedIssueWakeups), not through a cross-table trigger.
DROP TRIGGER IF EXISTS stop_issue_wakeups ON issue;
DROP FUNCTION IF EXISTS stop_issue_wakeups();
