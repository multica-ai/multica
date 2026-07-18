-- Validate the strict shadow constraint only after the attribution backfill has
-- completed. This takes a SHARE UPDATE EXCLUSIVE lock while scanning the table,
-- which permits normal INSERT/UPDATE/DELETE traffic to continue.
--
-- Any remaining row where originator_user_id is set but accountable_user_id is
-- NULL or different causes this migration to fail closed. The validated shadow
-- replaces migration 190's transitional constraint in migration 199.
ALTER TABLE agent_task_queue
    VALIDATE CONSTRAINT agent_task_queue_accountable_matches_originator_strict;
