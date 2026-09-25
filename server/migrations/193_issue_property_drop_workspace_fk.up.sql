-- A short-lived pre-release build created this foreign key in migration 191.
-- Fresh databases no longer create it; this idempotent cleanup brings databases
-- that applied the earlier migration back to the application-owned lifecycle.
ALTER TABLE issue_property
    DROP CONSTRAINT IF EXISTS issue_property_workspace_id_fkey;
