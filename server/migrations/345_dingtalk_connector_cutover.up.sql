-- Rolling deployments may still have old processes writing DingTalk rows to
-- channel_installation while this migration runs. Serialize the cutover with
-- those writers so every committed legacy change is included in the copy.
LOCK TABLE channel_installation IN SHARE ROW EXCLUSIVE MODE;

INSERT INTO dingtalk_connector (
    id, app_id, config, status, ws_lease_token, ws_lease_expires_at,
    installer_user_id, installed_at, created_at, updated_at
)
SELECT
    id, config ->> 'app_id', config, status, ws_lease_token,
    ws_lease_expires_at, installer_user_id, installed_at, created_at,
    updated_at
FROM channel_installation
WHERE channel_type = 'dingtalk';

INSERT INTO dingtalk_workspace_grant (
    connector_id, workspace_id, default_agent_id, installer_user_id,
    status, installed_at, created_at, updated_at
)
SELECT
    id, workspace_id, agent_id, installer_user_id, status, installed_at,
    created_at, updated_at
FROM channel_installation
WHERE channel_type = 'dingtalk';

-- Old processes must fail closed after the cutover. Without this gate an old
-- install request can recreate a legacy row after the copy; a new process may
-- then supervise both that row and dingtalk_connector and open two Streams for
-- one bot. The table lock above closes the race between the copy and trigger.
CREATE OR REPLACE FUNCTION reject_legacy_dingtalk_installation_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.channel_type = 'dingtalk'
        OR (TG_OP = 'UPDATE' AND OLD.channel_type = 'dingtalk')
    THEN
        RAISE EXCEPTION 'legacy DingTalk installation writes are disabled after connector cutover'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END
$$;

DROP TRIGGER IF EXISTS trg_reject_legacy_dingtalk_installation_write
    ON channel_installation;
CREATE TRIGGER trg_reject_legacy_dingtalk_installation_write
BEFORE INSERT OR UPDATE ON channel_installation
FOR EACH ROW
EXECUTE FUNCTION reject_legacy_dingtalk_installation_write();

DELETE FROM channel_installation
WHERE channel_type = 'dingtalk';
