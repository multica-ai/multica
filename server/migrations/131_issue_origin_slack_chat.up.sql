-- Extend issue.origin_type to allow the Slack `/issue` command paths (both the
-- message-prefix form and the slash command) to stamp issues with
-- origin_type='slack_chat'. The Slack integration shipped this origin label
-- (originSlackChat) but no migration ever added it to the CHECK list, so every
-- Slack /issue create tripped SQLSTATE 23514 and IssueService.Create failed —
-- surfaced end-to-end by the /issue slash command (MUL-3908). Mirrors 111
-- (lark_chat), which fixed the identical gap for Lark.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat'));
