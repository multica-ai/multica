-- Serialize rollback with connector/grant/route writers. Otherwise a new
-- process could add or revoke state after the guards below have checked it,
-- and the following table-down migrations would silently discard that state.
LOCK TABLE
    dingtalk_connector,
    dingtalk_workspace_grant,
    dingtalk_direct_route
IN SHARE ROW EXCLUSIVE MODE;

-- 358 down normally installs these guards before any contract migration. Keep
-- 345 independently safe too, because a database may have upgraded only
-- through 345. The connector trigger is installed after its active rows are
-- copied back and parked below; the table lock closes that intentional gap.
CREATE OR REPLACE FUNCTION reject_dingtalk_connector_rollback_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'DingTalk connector writes are disabled during rollback'
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS trg_reject_dingtalk_grant_rollback_write
    ON dingtalk_workspace_grant;
CREATE TRIGGER trg_reject_dingtalk_grant_rollback_write
BEFORE INSERT OR UPDATE OR DELETE ON dingtalk_workspace_grant
FOR EACH ROW
EXECUTE FUNCTION reject_dingtalk_connector_rollback_write();

DROP TRIGGER IF EXISTS trg_reject_dingtalk_direct_route_rollback_write
    ON dingtalk_direct_route;
CREATE TRIGGER trg_reject_dingtalk_direct_route_rollback_write
BEFORE INSERT OR UPDATE OR DELETE ON dingtalk_direct_route
FOR EACH ROW
EXECUTE FUNCTION reject_dingtalk_connector_rollback_write();

DROP TRIGGER IF EXISTS trg_reject_dingtalk_connector_rollback_write
    ON dingtalk_connector;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM dingtalk_connector connector
        LEFT JOIN dingtalk_workspace_grant grant_row
            ON grant_row.connector_id = connector.id
        GROUP BY connector.id
        HAVING count(grant_row.id) <> 1
    ) THEN
        RAISE EXCEPTION 'cannot roll back DingTalk connectors unless every connector has exactly one workspace grant';
    END IF;

    IF EXISTS (SELECT 1 FROM dingtalk_direct_route) THEN
        RAISE EXCEPTION 'cannot roll back DingTalk connectors while direct-message routes exist';
    END IF;

    DROP TRIGGER IF EXISTS trg_reject_legacy_dingtalk_installation_write
        ON channel_installation;
    DROP FUNCTION IF EXISTS reject_legacy_dingtalk_installation_write();

    INSERT INTO channel_installation (
        id, workspace_id, agent_id, channel_type, config, status,
        ws_lease_token, ws_lease_expires_at, installer_user_id,
        installed_at, created_at, updated_at
    )
    SELECT
        connector.id, grant_row.workspace_id, grant_row.default_agent_id,
        'dingtalk', connector.config, connector.status,
        connector.ws_lease_token, connector.ws_lease_expires_at,
        grant_row.installer_user_id, connector.installed_at,
        connector.created_at, connector.updated_at
    FROM dingtalk_connector connector
    JOIN dingtalk_workspace_grant grant_row
        ON grant_row.connector_id = connector.id
    WHERE NOT EXISTS (
        SELECT 1
        FROM channel_installation legacy
        WHERE legacy.id = connector.id
    );
END
$$;

-- The legacy row above preserves the pre-rollback status and lease. Park the
-- new connector only after that copy so it disappears from the still-running
-- new supervisor's next discovery sweep. The old supervisor waits on the same
-- UUID lease key before taking over.
UPDATE dingtalk_connector
SET status = 'revoked',
    updated_at = now()
WHERE status = 'active';

CREATE TRIGGER trg_reject_dingtalk_connector_rollback_write
BEFORE INSERT OR UPDATE OR DELETE ON dingtalk_connector
FOR EACH ROW
EXECUTE FUNCTION reject_dingtalk_connector_rollback_write();
