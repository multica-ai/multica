-- Set the first global workflow administrator.
UPDATE "user" SET can_manage_workflows = TRUE WHERE email = 'admin@example.com';
