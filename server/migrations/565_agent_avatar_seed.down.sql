-- Drop the generated-avatar seed. avatar_url is not modified, so legacy emoji
-- and uploaded image avatars are unaffected.
ALTER TABLE agent DROP COLUMN IF EXISTS avatar_seed;
