-- Extend issue.origin_type to allow the WeCom /issue command path to stamp
-- issues with origin_type='wecom_chat'. The WeCom integration sets this label
-- (originWecomChat in resolvers.go) but no migration had added it to the CHECK
-- list, so every WeCom /issue create tripped SQLSTATE 23514 and IssueService.Create
-- failed. Mirrors 131 (slack_chat) and 149 (agent_create).
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'wecom_chat'));
