-- Cannot reliably distinguish rows the migration flipped from rows the user
-- intentionally set to 'public' afterwards, so the rollback is a no-op.
SELECT 1;
