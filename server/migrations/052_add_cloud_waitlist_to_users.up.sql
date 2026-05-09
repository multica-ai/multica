-- RFC 5321 caps email addresses at 254 chars. Bounded TEXT prevents
-- the column being abused as arbitrary storage.
ALTER TABLE "user"
  ADD COLUMN cloud_waitlist_email VARCHAR(254),
  ADD COLUMN cloud_waitlist_reason TEXT;
