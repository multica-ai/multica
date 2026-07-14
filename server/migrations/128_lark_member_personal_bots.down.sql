DROP TABLE IF EXISTS lark_workspace_notification_policy;

DROP INDEX IF EXISTS uniq_lark_member_installation_workspace_user;

DELETE FROM lark_installation
WHERE installation_kind = 'member';

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_kind_target_check;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_installation_kind_check;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_member_user_fkey;

ALTER TABLE lark_installation
    DROP COLUMN IF EXISTS member_user_id;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_installation_kind_check
    CHECK (installation_kind IN ('agent', 'notification'));

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_kind_target_check
    CHECK (
        (installation_kind = 'agent' AND agent_id IS NOT NULL)
        OR (installation_kind = 'notification' AND agent_id IS NULL AND runtime_id IS NULL)
    );
