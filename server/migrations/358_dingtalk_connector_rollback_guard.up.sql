DO $$
BEGIN
    IF to_regclass('dingtalk_connector') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS trg_reject_dingtalk_connector_rollback_write
            ON dingtalk_connector;
    END IF;
    IF to_regclass('dingtalk_workspace_grant') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS trg_reject_dingtalk_grant_rollback_write
            ON dingtalk_workspace_grant;
    END IF;
    IF to_regclass('dingtalk_direct_route') IS NOT NULL THEN
        DROP TRIGGER IF EXISTS trg_reject_dingtalk_direct_route_rollback_write
            ON dingtalk_direct_route;
    END IF;
    DROP FUNCTION IF EXISTS reject_dingtalk_connector_rollback_write();
END
$$;
