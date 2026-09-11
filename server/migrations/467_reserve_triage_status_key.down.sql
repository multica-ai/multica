-- Drops only the reservation. Workspaces whose custom `triage` status was
-- renamed keep the replacement key; restoring `triage` would hand their issues
-- back to a key the Triage feature owns.
ALTER TABLE issue_status DROP CONSTRAINT IF EXISTS issue_status_key_not_reserved;
