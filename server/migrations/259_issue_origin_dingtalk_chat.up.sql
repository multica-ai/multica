-- Extend issue.origin_type for issues created through DingTalk's /issue
-- command. The shared channel Router stamps origin_type='dingtalk_chat' and
-- origin_id=<chat_session.id>; without this CHECK entry every DingTalk issue
-- creation fails with SQLSTATE 23514.
--
-- The CHECK is only WIDENED (one new allowed value), so every existing row
-- already satisfies it. Add it NOT VALID so this statement takes ACCESS
-- EXCLUSIVE on issue only briefly, without a full-table scan; migration 260
-- runs the VALIDATE under SHARE UPDATE EXCLUSIVE, which does not block reads
-- or writes. issue is a hot core table, so a plain validating ADD here would
-- block all traffic on it for the length of the scan.
--
-- Keep the VALIDATE in its own migration file (the 197/198 pattern), NOT
-- merely as a later statement in this one: the migration runner hands each
-- file to a single conn.Exec, so every statement in a file shares one implicit
-- transaction and the ACCESS EXCLUSIVE taken here would be held until the file
-- finishes — carrying the strong lock straight through the validation scan and
-- defeating the split.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'lark_chat', 'slack_chat', 'agent_create', 'dingtalk_chat'))
    NOT VALID;
