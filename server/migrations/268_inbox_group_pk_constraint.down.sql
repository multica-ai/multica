-- Dropping the constraint leaves the underlying unique index behind under the
-- constraint's name, which 267's down step then removes.
ALTER TABLE inbox_group DROP CONSTRAINT IF EXISTS inbox_group_pkey;
