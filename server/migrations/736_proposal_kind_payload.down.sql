-- Restores the origin CHECK without squad_request first. It fails while such
-- issues exist, which is the intent: they would otherwise lose their origin.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat', 'telegram_chat', 'proposal'))
    NOT VALID;

ALTER TABLE proposal DROP COLUMN IF EXISTS payload;
ALTER TABLE proposal DROP COLUMN IF EXISTS kind;
