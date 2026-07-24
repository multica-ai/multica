-- Extend issue.origin_type to allow the WeChat ClawBot `/issue` command path to
-- stamp issues with origin_type='wechat_chat'. The WeChat integration ships this
-- origin label (originWechatChat) but the CHECK list lacks it, so every WeChat
-- /issue create would trip SQLSTATE 23514 and IssueService.Create would fail —
-- the identical gap 131 fixed for Slack and 111 fixed for Lark. Mirrors 131.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'wechat_chat'));
