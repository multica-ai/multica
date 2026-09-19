ALTER TABLE comment
    DROP CONSTRAINT IF EXISTS comment_request_payload_format,
    DROP CONSTRAINT IF EXISTS comment_request_key_format,
    DROP COLUMN IF EXISTS request_payload_sha256,
    DROP COLUMN IF EXISTS request_key;
