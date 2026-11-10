DROP TABLE IF EXISTS issue_environment_config;
DROP TABLE IF EXISTS issue_environment_command;
ALTER TABLE issue_environment
    DROP COLUMN IF EXISTS controllable,
    DROP COLUMN IF EXISTS excerpt;
