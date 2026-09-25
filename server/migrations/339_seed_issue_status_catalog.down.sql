-- Drops only the seeded built-ins, leaving any custom statuses an admin
-- created. Deleting those would strand every issue sitting on one.
DELETE FROM issue_status WHERE is_system = TRUE;
