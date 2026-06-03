-- Rollback for 9058_cerebro_sprints.up.sql.
-- Drop children first; CASCADE protects against any forgotten FK.

DROP TABLE IF EXISTS cerebro_sprint_recurring_task;
DROP TABLE IF EXISTS cerebro_sprint_issue;
DROP TABLE IF EXISTS cerebro_sprint;
DROP TABLE IF EXISTS cerebro_sprint_settings;
