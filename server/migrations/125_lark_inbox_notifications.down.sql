DROP TABLE IF EXISTS lark_inbox_notification;
DROP INDEX IF EXISTS uniq_lark_notification_installation_workspace;
ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_kind_target_check;
DELETE FROM lark_installation WHERE installation_kind = 'notification';
ALTER TABLE lark_installation
    ALTER COLUMN agent_id SET NOT NULL;
ALTER TABLE lark_installation
    DROP COLUMN IF EXISTS installation_kind;
