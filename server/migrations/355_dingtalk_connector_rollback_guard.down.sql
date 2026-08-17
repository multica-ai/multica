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
