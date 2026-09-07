-- Keep accepting writes from an older server process during a mixed-version
-- rollout. Old discovery writes do not expose bot identity, but they must not
-- disappear after the one-time backfill. A route revision change is an admin
-- routing edit, not observed message activity, so it only refreshes the title.
CREATE OR REPLACE FUNCTION mirror_legacy_dingtalk_group_route_presence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    is_activity BOOLEAN;
BEGIN
    is_activity := TG_OP = 'INSERT';
    IF TG_OP = 'UPDATE' THEN
        is_activity := NEW.revision = OLD.revision;
    END IF;

    INSERT INTO dingtalk_group_presence (
        workspace_id,
        installation_id,
        conversation_id,
        conversation_title,
        first_seen_at,
        last_active_at,
        mention_count
    ) VALUES (
        NEW.workspace_id,
        NEW.installation_id,
        NEW.conversation_id,
        NEW.conversation_title,
        NEW.discovered_at,
        CASE WHEN is_activity THEN now() ELSE NULL END,
        CASE WHEN is_activity THEN 1 ELSE 0 END
    )
    ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
        workspace_id = EXCLUDED.workspace_id,
        conversation_title = CASE
            WHEN EXCLUDED.conversation_title <> '' THEN EXCLUDED.conversation_title
            ELSE dingtalk_group_presence.conversation_title
        END,
        first_seen_at = LEAST(dingtalk_group_presence.first_seen_at, EXCLUDED.first_seen_at),
        last_active_at = CASE
            WHEN is_activity THEN now()
            ELSE dingtalk_group_presence.last_active_at
        END,
        mention_count = dingtalk_group_presence.mention_count + CASE WHEN is_activity THEN 1 ELSE 0 END,
        updated_at = now();

    RETURN NEW;
END;
$$;

DO $$
BEGIN
    IF to_regclass('dingtalk_group_route') IS NOT NULL THEN
        EXECUTE 'CREATE TRIGGER mirror_legacy_dingtalk_group_route_presence
            AFTER INSERT OR UPDATE ON dingtalk_group_route
            FOR EACH ROW
            EXECUTE FUNCTION mirror_legacy_dingtalk_group_route_presence()';
    END IF;
END;
$$;

-- Historical route rows prove that a bot was discovered in a group and retain
-- its title. They do not prove when a message was last processed, so activity
-- remains unknown until a new addressed message succeeds.
-- A draft build briefly persisted observed titles in installation config. This
-- source also makes upgrades from that build recoverable if it already dropped
-- the legacy route table.
INSERT INTO dingtalk_group_presence (
    workspace_id, installation_id, conversation_id, conversation_title,
    bot_name, bot_identity_issue
)
SELECT
    installation.workspace_id,
    installation.id,
    groups.conversation_id,
    groups.conversation_title,
    COALESCE(installation.config -> 'group_bot_names' ->> groups.conversation_id, ''),
    COALESCE(installation.config -> 'group_bot_identity_issues' ->> groups.conversation_id, '')
FROM channel_installation installation
CROSS JOIN LATERAL jsonb_each_text(
    CASE
        WHEN jsonb_typeof(installation.config -> 'group_titles') = 'object'
            THEN installation.config -> 'group_titles'
        ELSE '{}'::jsonb
    END
) AS groups(conversation_id, conversation_title)
WHERE installation.channel_type = 'dingtalk'
ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
    conversation_title = CASE
        WHEN dingtalk_group_presence.conversation_title = '' AND EXCLUDED.conversation_title <> ''
            THEN EXCLUDED.conversation_title
        ELSE dingtalk_group_presence.conversation_title
    END,
    bot_name = CASE
        WHEN EXCLUDED.bot_name <> '' THEN EXCLUDED.bot_name
        ELSE dingtalk_group_presence.bot_name
    END,
    bot_identity_issue = CASE
        WHEN EXCLUDED.bot_identity_issue <> '' THEN EXCLUDED.bot_identity_issue
        ELSE dingtalk_group_presence.bot_identity_issue
    END,
    updated_at = now();

DO $$
BEGIN
    IF to_regclass('dingtalk_group_route') IS NOT NULL THEN
        EXECUTE $backfill$
            INSERT INTO dingtalk_group_presence (
                workspace_id, installation_id, conversation_id,
                conversation_title, first_seen_at
            )
            SELECT
                workspace_id, installation_id, conversation_id,
                conversation_title, discovered_at
            FROM dingtalk_group_route
            ON CONFLICT (installation_id, conversation_id) DO UPDATE SET
                conversation_title = CASE
                    WHEN dingtalk_group_presence.conversation_title = ''
                         AND EXCLUDED.conversation_title <> ''
                        THEN EXCLUDED.conversation_title
                    ELSE dingtalk_group_presence.conversation_title
                END,
                first_seen_at = LEAST(
                    dingtalk_group_presence.first_seen_at,
                    EXCLUDED.first_seen_at
                ),
                updated_at = now()
        $backfill$;
    END IF;
END;
$$;
