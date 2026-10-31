-- Preserve the existing registration-error placeholder behavior unless a
-- workspace explicitly opts a profile into host-local found-only registration.
ALTER TABLE runtime_profile
    ADD COLUMN skip_if_missing BOOLEAN NOT NULL DEFAULT false;
