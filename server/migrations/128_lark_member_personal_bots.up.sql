-- A member-owned Lark Bot is the member's representative in Multica.
-- Each workspace member can have at most one active installation record;
-- removing the member also removes their installation and bindings.
ALTER TABLE lark_installation
    ADD COLUMN member_user_id UUID;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_member_user_fkey
    FOREIGN KEY (workspace_id, member_user_id)
    REFERENCES member(workspace_id, user_id)
    ON DELETE CASCADE;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_installation_kind_check;

ALTER TABLE lark_installation
    DROP CONSTRAINT IF EXISTS lark_installation_kind_target_check;

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_installation_kind_check
    CHECK (installation_kind IN ('agent', 'notification', 'member'));

ALTER TABLE lark_installation
    ADD CONSTRAINT lark_installation_kind_target_check
    CHECK (
        (installation_kind = 'agent' AND agent_id IS NOT NULL AND member_user_id IS NULL)
        OR (installation_kind = 'notification' AND agent_id IS NULL AND runtime_id IS NULL AND member_user_id IS NULL)
        OR (installation_kind = 'member' AND agent_id IS NULL AND runtime_id IS NULL AND member_user_id IS NOT NULL)
    );

CREATE UNIQUE INDEX uniq_lark_member_installation_workspace_user
    ON lark_installation(workspace_id, member_user_id)
    WHERE installation_kind = 'member';

-- Event preferences belong to the workspace, not to the Bot that happens to
-- deliver them. Existing notification-Bot settings are preserved as the
-- initial workspace policy during the upgrade.
CREATE TABLE lark_workspace_notification_policy (
    workspace_id UUID PRIMARY KEY REFERENCES workspace(id) ON DELETE CASCADE,
    event_types TEXT[] NOT NULL DEFAULT ARRAY[
        'issue_assigned',
        'mentioned',
        'task_failed',
        'quick_create_failed',
        'autopilot_paused'
    ]::TEXT[],
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO lark_workspace_notification_policy (workspace_id, event_types)
SELECT workspace_id, notification_event_types
FROM lark_installation
WHERE installation_kind = 'notification'
ON CONFLICT (workspace_id) DO NOTHING;
