-- Extend issue.origin_type to allow the WeCom smart-bot (aibot) `/issue`
-- command path to stamp issues with origin_type='wecom_chat' + origin_id=
-- <chat_session.id>. Mirrors 111_issue_origin_lark_chat and
-- 131_issue_origin_slack_chat — same origin_id semantics (the chat_session
-- the /issue command was typed in), different label because analytics and
-- inbound routing key on this string.
--
-- The full list is respecified (not just appended) because ADD CONSTRAINT
-- cannot append a value. It must therefore carry every value added by earlier
-- migrations — including 'dingtalk_chat' (migration 259) — or this rebuild
-- would silently drop it. Keep this list in sync with the newest
-- issue_origin_* migration when rebasing.
--
-- The CHECK is only WIDENED (one new allowed value), so every existing row
-- already satisfies it. Add it NOT VALID so this statement takes ACCESS
-- EXCLUSIVE on issue only briefly, without a full-table scan; migration 264
-- runs the VALIDATE under SHARE UPDATE EXCLUSIVE, which does not block reads
-- or writes. issue is a hot core table, so a plain validating ADD here would
-- block all traffic on it for the length of the scan.
--
-- Keep the VALIDATE in its own migration file (the 259/260 pattern), NOT
-- merely as a later statement in this one: the migration runner hands each
-- file to a single conn.Exec, so every statement in a file shares one implicit
-- transaction and the ACCESS EXCLUSIVE taken here would be held until the file
-- finishes — carrying the strong lock straight through the validation scan and
-- defeating the split.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat', 'wecom_chat'))
    NOT VALID;
