-- The muted auto_archive_on_* keys stay: code before 718 ignores them, and they
-- may already be a member's own choice.
UPDATE notification_preference
SET preferences = preferences - 'auto_archive_prompt'
WHERE preferences ? 'auto_archive_prompt';
