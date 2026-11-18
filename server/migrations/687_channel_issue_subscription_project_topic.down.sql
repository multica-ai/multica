ALTER TABLE channel_issue_subscription
    DROP COLUMN IF EXISTS muted,
    DROP COLUMN IF EXISTS project_subscription_id;
