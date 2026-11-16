-- One subscription per (issue, bot, chat, thread); also serves the per-issue
-- fanout lookup through its leading issue_id column.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_channel_issue_subscription_target
    ON channel_issue_subscription (issue_id, installation_id, chat_id, thread_id);
