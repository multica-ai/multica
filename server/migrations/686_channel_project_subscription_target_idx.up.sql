-- One subscription per (project, bot, chat, thread); also serves the
-- per-project fanout lookup through its leading project_id column.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_channel_project_subscription_target
    ON channel_project_subscription (project_id, installation_id, chat_id, thread_id);
