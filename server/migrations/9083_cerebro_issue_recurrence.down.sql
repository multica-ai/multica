-- Rollback for 9083_cerebro_issue_recurrence.up.sql.
-- Drop children first; CASCADE protects against any forgotten FK.

DROP TABLE IF EXISTS cerebro_issue_recurrence_log;
DROP TABLE IF EXISTS cerebro_issue_recurrence;
