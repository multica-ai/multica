ALTER TABLE project
    DROP COLUMN IF EXISTS archived_by_id,
    DROP COLUMN IF EXISTS archived_by_type,
    DROP COLUMN IF EXISTS archived_at;
