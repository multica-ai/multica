-- Re-home `archived` issues before rolling back migration 213.
--
-- The 213 down-migration refuses to run while any row has status='archived'
-- (restoring the enum would otherwise violate the CHECK).  Run this sweep to
-- move archived issues to `cancelled`, then apply the down migration.
--
-- `cancelled` is the closest terminal, non-completed-visible status; adjust the
-- target here if product prefers a different re-homing (e.g. `done`).
UPDATE issue SET status = 'cancelled' WHERE status = 'archived';
