-- During a mixed-version rollout, a process from the previous draft may still
-- write identity values to group-presence rows. Mirror those writes into the
-- installation-level identity without treating empty transient lookups as new
-- information.
CREATE OR REPLACE FUNCTION mirror_dingtalk_group_presence_bot_identity()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.bot_name = '' AND NEW.bot_identity_issue = '' THEN
        RETURN NEW;
    END IF;

    IF EXISTS (
        SELECT 1
        FROM dingtalk_bot_identity identity
        WHERE identity.installation_id = NEW.installation_id
          AND identity.workspace_id = NEW.workspace_id
          AND identity.bot_name = NEW.bot_name
          AND identity.bot_identity_issue = NEW.bot_identity_issue
    ) THEN
        RETURN NEW;
    END IF;

    INSERT INTO dingtalk_bot_identity (
        workspace_id, installation_id, bot_name, bot_identity_issue
    ) VALUES (
        NEW.workspace_id, NEW.installation_id, NEW.bot_name, NEW.bot_identity_issue
    )
    ON CONFLICT (installation_id) DO UPDATE SET
        workspace_id = EXCLUDED.workspace_id,
        bot_name = CASE
            WHEN EXCLUDED.bot_name <> '' THEN EXCLUDED.bot_name
            ELSE dingtalk_bot_identity.bot_name
        END,
        bot_identity_issue = CASE
            WHEN EXCLUDED.bot_identity_issue <> '' THEN EXCLUDED.bot_identity_issue
            WHEN EXCLUDED.bot_name <> '' THEN ''
            ELSE dingtalk_bot_identity.bot_identity_issue
        END,
        updated_at = now();

    RETURN NEW;
END;
$$;

CREATE TRIGGER mirror_dingtalk_group_presence_bot_identity
AFTER INSERT OR UPDATE OF bot_name, bot_identity_issue ON dingtalk_group_presence
FOR EACH ROW
EXECUTE FUNCTION mirror_dingtalk_group_presence_bot_identity();

-- Prefer normalized observations, then recover identity written by the brief
-- config-backed draft. Empty rows are still useful: they preserve the fact that
-- an installation has been observed without inventing a readable Bot name.
INSERT INTO dingtalk_bot_identity (
    workspace_id, installation_id, bot_name, bot_identity_issue
)
SELECT
    installation.workspace_id,
    installation.id,
    COALESCE(presence.bot_name, config_name.bot_name, ''),
    COALESCE(presence.bot_identity_issue, config_issue.bot_identity_issue, '')
FROM channel_installation installation
LEFT JOIN LATERAL (
    SELECT
        max(NULLIF(bot_name, ''))::text AS bot_name,
        max(NULLIF(bot_identity_issue, ''))::text AS bot_identity_issue
    FROM dingtalk_group_presence
    WHERE installation_id = installation.id
) presence ON true
LEFT JOIN LATERAL (
    SELECT max(NULLIF(value, ''))::text AS bot_name
    FROM jsonb_each_text(
        CASE
            WHEN jsonb_typeof(installation.config -> 'group_bot_names') = 'object'
                THEN installation.config -> 'group_bot_names'
            ELSE '{}'::jsonb
        END
    )
) config_name ON true
LEFT JOIN LATERAL (
    SELECT max(NULLIF(value, ''))::text AS bot_identity_issue
    FROM jsonb_each_text(
        CASE
            WHEN jsonb_typeof(installation.config -> 'group_bot_identity_issues') = 'object'
                THEN installation.config -> 'group_bot_identity_issues'
            ELSE '{}'::jsonb
        END
    )
) config_issue ON true
WHERE installation.channel_type = 'dingtalk'
ON CONFLICT (installation_id) DO UPDATE SET
    workspace_id = EXCLUDED.workspace_id,
    bot_name = CASE
        WHEN EXCLUDED.bot_name <> '' THEN EXCLUDED.bot_name
        ELSE dingtalk_bot_identity.bot_name
    END,
    bot_identity_issue = CASE
        WHEN EXCLUDED.bot_identity_issue <> '' THEN EXCLUDED.bot_identity_issue
        ELSE dingtalk_bot_identity.bot_identity_issue
    END,
    updated_at = now();
