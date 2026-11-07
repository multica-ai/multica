-- Persist with the business transition: in-process bus subscribers alone lose
-- notifications when the publisher exits between commit and event delivery.
CREATE FUNCTION queue_channel_issue_notice(target UUID, notice_kind TEXT, failed_task UUID) RETURNS void
LANGUAGE plpgsql AS $$
DECLARE
    source_issue RECORD;
BEGIN
    SELECT i.id, i.workspace_id, i.revision, i.status, i.creator_id, i.origin_id
    INTO source_issue FROM issue i
    WHERE i.id = target AND i.parent_issue_id IS NULL AND i.origin_type = 'agent_create'
      AND i.creator_type = 'agent';
    IF NOT FOUND THEN RETURN; END IF;

    INSERT INTO channel_issue_notice (
        workspace_id, issue_id, issue_revision, event_key, kind, status_key,
        failed_task_id, source_task_id, binding_id, installation_id,
        binding_generation, recipient_user_id, channel_sender_id, channel_chat_id,
        channel_message_id, channel_thread_id, route_config, retry_at
    )
    SELECT source_issue.workspace_id, source_issue.id, source_issue.revision,
        CASE WHEN failed_task IS NULL THEN 'status:' || source_issue.revision::text
             ELSE 'failure:' || failed_task::text END,
        notice_kind, source_issue.status, failed_task, d.task_id, d.binding_id, ci.id,
        COALESCE(ci.config->>'binding_generation', ''), t.originator_user_id,
        d.channel_sender_id, d.channel_chat_id, d.channel_message_id, d.channel_thread_id,
        d.config, now() + CASE WHEN failed_task IS NULL THEN interval '0 seconds' ELSE interval '30 seconds' END
    FROM agent_task_queue t
    JOIN channel_task_delivery d ON d.task_id = COALESCE(t.chat_input_task_id, t.id)
    JOIN channel_installation ci ON ci.id = d.installation_id
        AND ci.workspace_id = source_issue.workspace_id AND ci.agent_id = source_issue.creator_id
        AND ci.channel_type = 'feishu' AND ci.status = 'active'
        -- A replacement app must not inherit a previous app's conversation.
        AND ci.installed_at <= d.created_at
    JOIN agent a ON a.id = ci.agent_id AND a.workspace_id = source_issue.workspace_id AND a.archived_at IS NULL
    JOIN chat_session cs ON cs.id = t.chat_session_id AND cs.workspace_id = source_issue.workspace_id
    JOIN channel_chat_session_binding cb ON cb.id = d.binding_id AND cb.installation_id = ci.id
        AND cb.chat_session_id = cs.id AND cb.channel_chat_id = d.channel_chat_id
        AND cb.channel_type = 'feishu' AND cb.chat_type = 'group'
    JOIN channel_user_binding ub ON ub.installation_id = ci.id AND ub.workspace_id = source_issue.workspace_id
        AND ub.channel_type = 'feishu' AND ub.channel_user_id = d.channel_sender_id
        AND ub.multica_user_id = t.originator_user_id
    JOIN member m ON m.workspace_id = source_issue.workspace_id AND m.user_id = t.originator_user_id
    WHERE t.id = source_issue.origin_id AND t.agent_id = source_issue.creator_id
        AND d.channel_type = 'feishu' AND d.chat_type = 'group'
        AND NULLIF(d.channel_sender_id, '') IS NOT NULL
    ON CONFLICT (issue_id, event_key) DO NOTHING;
END;
$$;

CREATE FUNCTION capture_channel_issue_status_notice() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'blocked' THEN
        PERFORM queue_channel_issue_notice(NEW.id, 'blocked', NULL);
    ELSIF (NEW.status = 'done' OR EXISTS (
        SELECT 1 FROM issue_status s WHERE s.workspace_id = NEW.workspace_id AND s.key = NEW.status AND s.category = 'done'
    )) AND NOT (OLD.status = 'done' OR EXISTS (
        SELECT 1 FROM issue_status s WHERE s.workspace_id = OLD.workspace_id AND s.key = OLD.status AND s.category = 'done'
    )) THEN
        PERFORM queue_channel_issue_notice(NEW.id, 'completed', NULL);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER capture_channel_issue_status_notice AFTER UPDATE OF status ON issue
FOR EACH ROW WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION capture_channel_issue_status_notice();

CREATE FUNCTION capture_channel_issue_failure_notice() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM issue i WHERE i.id = NEW.issue_id AND i.status NOT IN ('done', 'blocked', 'cancelled')
        AND NOT EXISTS (SELECT 1 FROM issue_status s WHERE s.workspace_id = i.workspace_id AND s.key = i.status AND s.category IN ('done', 'closed'))
    ) THEN
        PERFORM queue_channel_issue_notice(NEW.issue_id, 'failed', NEW.id);
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER capture_channel_issue_failure_notice AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW WHEN (NEW.status = 'failed' AND OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION capture_channel_issue_failure_notice();
