-- A chat that follows a project gets one topic per issue on platforms whose
-- chat-level posts root a topic (Feishu): the first update of an issue opens
-- it, and a channel_issue_subscription row records its anchor message with
-- project_subscription_id pointing at the project subscription it came from.
-- Such a row is dropped once that project subscription is gone or the issue
-- leaves the project. muted keeps an issue's topic quiet after
-- /unsubscribe issue inside it, without the project reopening a new topic.
ALTER TABLE channel_issue_subscription
    ADD COLUMN IF NOT EXISTS project_subscription_id UUID,
    ADD COLUMN IF NOT EXISTS muted BOOLEAN NOT NULL DEFAULT false;
