DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM channel_installation installation
        LEFT JOIN squad s
          ON s.id = installation.target_id
         AND s.workspace_id = installation.workspace_id
        LEFT JOIN agent a
          ON a.id = s.leader_id
         AND a.workspace_id = installation.workspace_id
        WHERE installation.target_type = 'squad'
          AND (s.id IS NULL OR a.id IS NULL)
    ) THEN
        RAISE EXCEPTION 'cannot roll back DingTalk Squad targets without a resolvable Leader';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM dingtalk_group_route route
        LEFT JOIN squad s
          ON s.id = route.target_id
         AND s.workspace_id = route.workspace_id
        LEFT JOIN agent a
          ON a.id = s.leader_id
         AND a.workspace_id = route.workspace_id
        WHERE route.target_type = 'squad'
          AND (s.id IS NULL OR a.id IS NULL)
    ) THEN
        RAISE EXCEPTION 'cannot roll back DingTalk Squad routes without a resolvable Leader';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM (
            SELECT installation.workspace_id,
                   installation.channel_type,
                   CASE WHEN installation.target_type = 'squad'
                        THEN s.leader_id ELSE installation.agent_id END AS resolved_agent_id
            FROM channel_installation installation
            LEFT JOIN squad s
              ON installation.target_type = 'squad'
             AND s.id = installation.target_id
             AND s.workspace_id = installation.workspace_id
        ) resolved
        GROUP BY resolved.workspace_id, resolved.channel_type, resolved.resolved_agent_id
        HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot roll back DingTalk Squad targets because resolved installation owners would collide';
    END IF;
END
$$;

UPDATE channel_installation installation
SET agent_id = s.leader_id
FROM squad s
WHERE installation.target_type = 'squad'
  AND s.id = installation.target_id
  AND s.workspace_id = installation.workspace_id;

UPDATE dingtalk_group_route route
SET agent_id = s.leader_id
FROM squad s
WHERE route.target_type = 'squad'
  AND s.id = route.target_id
  AND s.workspace_id = route.workspace_id;

ALTER TABLE dingtalk_group_route
    DROP COLUMN target_id,
    DROP COLUMN target_type;

ALTER TABLE channel_installation
    DROP COLUMN target_id,
    DROP COLUMN target_type;
