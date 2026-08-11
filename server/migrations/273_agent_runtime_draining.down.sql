-- Revert the CHECK to the pre-NEX-38 two-value set. Rows still holding
-- 'draining' at rollback time would violate the narrowed constraint, so
-- flush them to 'offline' first (drain is an intentional shutdown state;
-- on rollback the runtime must not silently become claimable).
UPDATE agent_runtime SET status = 'offline' WHERE status = 'draining';
ALTER TABLE agent_runtime DROP CONSTRAINT IF EXISTS agent_runtime_status_check;
ALTER TABLE agent_runtime ADD CONSTRAINT agent_runtime_status_check
    CHECK (status IN ('online', 'offline'));
