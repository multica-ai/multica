-- The content hash of a user's upload, so the same file is transferred once.
ALTER TABLE attachment ADD COLUMN sha256 TEXT NOT NULL DEFAULT '';
CREATE INDEX attachment_upload_hash ON attachment (scope_id, sha256);
