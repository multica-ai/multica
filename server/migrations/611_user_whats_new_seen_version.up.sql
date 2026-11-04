-- Newest release whose "What's new" highlights the user has seen, as
-- MAJOR.MINOR.PATCH. NULL means the user has never dismissed the card; the
-- client then counts releases dated after the account was created.
ALTER TABLE "user" ADD COLUMN IF NOT EXISTS whats_new_seen_version TEXT;
