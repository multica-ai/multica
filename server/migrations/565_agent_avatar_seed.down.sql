-- Drop the generated-avatar seed. avatar_url is not modified, so legacy emoji
-- and uploaded image avatars are unaffected.
--
-- Re-applying 565 after this regenerates every seed (the values are random
-- and are discarded with the column), so agents showing a generated avatar
-- can show a different one afterwards. A backend binary that selects
-- avatar_seed will fail its agent queries while this is rolled back.
ALTER TABLE agent DROP COLUMN IF EXISTS avatar_seed;
