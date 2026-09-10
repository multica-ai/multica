ALTER TABLE issue_lifecycle_status
    DROP CONSTRAINT IF EXISTS issue_lifecycle_status_spec_key_format,
    DROP COLUMN IF EXISTS spec_key;

ALTER TABLE issue_lifecycle
    DROP COLUMN IF EXISTS initial_status_id;
