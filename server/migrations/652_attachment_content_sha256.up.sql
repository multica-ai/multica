-- Hex SHA-256 of the uploaded bytes, recorded by the upload endpoint. Clients
-- that keep a local copy of an attachment compare against it before reading
-- that copy instead of downloading. NULL for rows written before this column
-- and by paths that never see the bytes; those always download.
ALTER TABLE attachment ADD COLUMN IF NOT EXISTS content_sha256 TEXT;
