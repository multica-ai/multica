-- Directive origin (spec P4): the Secretary's issue for a leader, created from
-- a live boss chat message. origin_id is that message's id. The CHECK is
-- recreated NOT VALID, as in 736, so the table scan does not run under the
-- ACCESS EXCLUSIVE lock.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'proposal', 'squad_request', 'directive'))
    NOT VALID;
