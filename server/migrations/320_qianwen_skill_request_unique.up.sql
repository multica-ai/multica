-- Single statement: CREATE UNIQUE INDEX CONCURRENTLY cannot run inside a
-- transaction or share a multi-command migration file. This is the conflict
-- arbiter for ClaimQianwenRequest and the durable external idempotency key.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_qianwen_skill_request_installation_request
    ON qianwen_skill_request (installation_id, request_id);
