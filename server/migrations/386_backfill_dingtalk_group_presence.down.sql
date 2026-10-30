DO $$
BEGIN
    IF to_regclass('dingtalk_group_route') IS NOT NULL THEN
        EXECUTE 'DROP TRIGGER IF EXISTS mirror_legacy_dingtalk_group_route_presence ON dingtalk_group_route';
    END IF;
END;
$$;
DROP FUNCTION IF EXISTS mirror_legacy_dingtalk_group_route_presence();
