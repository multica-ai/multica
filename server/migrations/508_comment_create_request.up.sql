ALTER TABLE comment
    ADD COLUMN request_key TEXT,
    ADD COLUMN request_payload_sha256 TEXT;

ALTER TABLE comment
    ADD CONSTRAINT comment_request_key_format
        CHECK (request_key IS NULL OR request_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,199}$'),
    ADD CONSTRAINT comment_request_payload_format
        CHECK (
            (request_key IS NULL AND request_payload_sha256 IS NULL)
            OR
            (request_key IS NOT NULL AND request_payload_sha256 ~ '^[0-9a-f]{64}$')
        );
