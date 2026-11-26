-- Moves the accounts on the hard-coded emergency denylist
-- (internal/auth/temporary_disabled_users.go, MUL-10280) onto suspended_at.
-- The email half of that list stays in code as a pre-signup gate until every
-- address is confirmed to belong to a suspended account. Matches nothing on
-- other installs.
UPDATE "user"
SET suspended_at = now(),
    suspended_reason = 'MUL-10280: migrated from the temporary denylist',
    suspended_by = 'migration'
WHERE suspended_at IS NULL
  AND (id IN ('514492f7-b30f-4147-bd33-c0e8ce5d6d4f', '1d542296-17c6-484a-9914-dcee589be116')
       OR lower(email) IN ('pdzzer68@embassybase.com', 'gtwtrox@mowan666.com'));
