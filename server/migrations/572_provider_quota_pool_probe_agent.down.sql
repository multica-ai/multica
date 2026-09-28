ALTER TABLE provider_quota_pool DROP COLUMN probe_agent_id;
-- Preserve append-only probe selection events. The expanded event check is
-- safe for the previous application version and permits a later re-apply.
