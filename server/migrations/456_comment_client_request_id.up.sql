-- Idempotency key for comment creation. Agents retry far more than humans; a
-- caller that passes the same client_request_id on a retry gets the
-- already-created comment back instead of posting a duplicate. NULL for every
-- comment created without a key (the default for all existing paths).
ALTER TABLE comment ADD COLUMN IF NOT EXISTS client_request_id TEXT;
