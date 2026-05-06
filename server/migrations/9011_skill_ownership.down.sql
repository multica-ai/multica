DROP TABLE IF EXISTS skill_fork;
DROP TABLE IF EXISTS skill_change_request;
DROP TABLE IF EXISTS skill_version;

ALTER TABLE skill
    DROP COLUMN IF EXISTS current_version,
    DROP COLUMN IF EXISTS approver_ids,
    DROP COLUMN IF EXISTS owner_id;
