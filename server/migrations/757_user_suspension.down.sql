DROP TABLE IF EXISTS user_suspension_event;
ALTER TABLE "user"
    DROP COLUMN IF EXISTS token_version,
    DROP COLUMN IF EXISTS suspended_by,
    DROP COLUMN IF EXISTS suspended_reason,
    DROP COLUMN IF EXISTS suspended_at;
