-- Proposal kinds (spec 3.2). kind selects the handler that validates a filing
-- and applies its approval; payload carries that kind's fields. Existing rows
-- are decisions with an empty payload, so phase 1 and 2 behaviour is unchanged.
--
-- The origin widening lets a squad's request to another squad be tagged
-- squad_request (spec 3.5). As in 616 the CHECK is recreated NOT VALID, so the
-- table scan does not run under the ACCESS EXCLUSIVE lock.
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'decision';
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS payload JSONB NOT NULL DEFAULT '{}';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'proposal', 'squad_request'))
    NOT VALID;
