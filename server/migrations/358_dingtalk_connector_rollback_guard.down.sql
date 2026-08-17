-- This is the highest migration in the connector series, so park every new
-- writer before any rollback migration removes unique indexes or tables. The
-- triggers remain until each table is dropped; 342 down removes the shared
-- function after the final table is gone. Reapplying this file is safe when a
-- runner committed the SQL but failed before updating schema_migrations.
LOCK TABLE
    dingtalk_connector,
    dingtalk_workspace_grant,
    dingtalk_direct_route
IN SHARE ROW EXCLUSIVE MODE;

CREATE OR REPLACE FUNCTION reject_dingtalk_connector_rollback_write()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'DingTalk connector writes are disabled during rollback'
        USING ERRCODE = '55000';
END
$$;

DROP TRIGGER IF EXISTS trg_reject_dingtalk_connector_rollback_write
    ON dingtalk_connector;
CREATE TRIGGER trg_reject_dingtalk_connector_rollback_write
BEFORE INSERT OR UPDATE OR DELETE ON dingtalk_connector
FOR EACH ROW
EXECUTE FUNCTION reject_dingtalk_connector_rollback_write();

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
END
$$;
