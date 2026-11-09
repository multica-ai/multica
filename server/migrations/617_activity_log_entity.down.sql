DELETE FROM activity_log WHERE entity_type = 'project';

ALTER TABLE activity_log
    DROP CONSTRAINT IF EXISTS activity_log_issue_entity_check,
    DROP CONSTRAINT IF EXISTS activity_log_entity_pair_check,
    DROP CONSTRAINT IF EXISTS activity_log_entity_type_check,
    DROP COLUMN IF EXISTS entity_id,
    DROP COLUMN IF EXISTS entity_type;
