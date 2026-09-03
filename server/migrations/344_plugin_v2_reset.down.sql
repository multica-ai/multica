-- The dropped V1 plugin schema is not restorable here: it spanned migrations
-- 285-326 with append-only triggers, a snapshot compiler, and an execution
-- manifest trigger on agent_task_queue. Rolling back means running those
-- migrations again. This direction only removes what the up direction created.

DROP TABLE IF EXISTS plugin_secret;
DROP TABLE IF EXISTS plugin_storage;
DROP TABLE IF EXISTS plugin_installation;
