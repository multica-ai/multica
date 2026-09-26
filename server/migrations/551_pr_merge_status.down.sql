-- Drops the setting entirely, including values admins chose after the up
-- migration; the previous release does not read it.
UPDATE workspace
SET settings = settings - 'pr_merge_status'
WHERE jsonb_typeof(settings) = 'object' AND settings ? 'pr_merge_status';
