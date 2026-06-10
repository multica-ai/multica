-- MUL-2553: extend issue.origin_type to allow 'agent_task' so an agent-created
-- issue can be stamped with origin_id=<agent_task_queue.id>. The subscriber
-- listener uses this to resolve agent_task_queue.original_user_id and
-- auto-subscribe the human who started the chain — without it, agent-created
-- issues only have creator_type='agent' and disappear from the human's inbox.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval', 'agent_task', 'lark_chat'));

-- Also widen the issue_subscriber.reason CHECK so the listener can record the
-- new 'triggered_agent' provenance. Members can still opt out per-reason via
-- preferences.auto_subscribe.triggered_agent — see subscriber_listeners.go.
ALTER TABLE issue_subscriber DROP CONSTRAINT IF EXISTS issue_subscriber_reason_check;
ALTER TABLE issue_subscriber ADD CONSTRAINT issue_subscriber_reason_check
    CHECK (reason IN ('creator', 'assignee', 'commenter', 'mentioned', 'manual', 'triggered_agent'));
