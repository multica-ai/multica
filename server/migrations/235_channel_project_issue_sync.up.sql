CREATE TABLE channel_project_binding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    installation_id UUID NOT NULL,
    channel_type TEXT NOT NULL DEFAULT 'feishu',
    channel_chat_id TEXT,
    channel_chat_name TEXT,
    state TEXT NOT NULL DEFAULT 'pending_group'
        CHECK (state IN ('pending_group', 'active', 'unbound', 'bot_revoked', 'bot_removed')),
    bind_token_hash TEXT,
    bind_token_expires_at TIMESTAMPTZ,
    created_by_user_id UUID NOT NULL,
    bound_by_user_id UUID,
    unbound_by_user_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    bound_at TIMESTAMPTZ,
    unbound_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (state <> 'active' OR (channel_chat_id IS NOT NULL AND channel_chat_name IS NOT NULL))
);

CREATE TABLE channel_issue_topic_binding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_binding_id UUID NOT NULL,
    project_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    channel_chat_id TEXT NOT NULL,
    topic_root_message_id TEXT NOT NULL,
    channel_thread_id TEXT,
    binding_source TEXT NOT NULL
        CHECK (binding_source IN ('issue_created_by_multica', 'issue_created_in_topic', 'manual_topic_bind', 'project_backfill')),
    state TEXT NOT NULL DEFAULT 'active'
        CHECK (state IN ('active', 'manual_unbound', 'project_unbound', 'orphaned', 'replaced')),
    created_by_user_id UUID,
    unbound_by_user_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    unbound_at TIMESTAMPTZ
);

CREATE TABLE channel_notification_outbox (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    event_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    project_binding_id UUID,
    issue_id UUID NOT NULL,
    task_id UUID,
    event_type TEXT NOT NULL
        CHECK (event_type IN ('issue_created', 'issue_status_changed', 'task_failed', 'task_cancelled')),
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'sending', 'sent', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    locked_by TEXT,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ
);

CREATE OR REPLACE FUNCTION enqueue_channel_issue_notification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_binding_id UUID;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.project_id IS DISTINCT FROM OLD.project_id THEN
        UPDATE channel_issue_topic_binding
        SET state = 'project_unbound',
            unbound_at = now(),
            updated_at = now()
        WHERE workspace_id = NEW.workspace_id
          AND issue_id = NEW.id
          AND state = 'active';
    END IF;

    IF NEW.project_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT id
    INTO target_binding_id
    FROM channel_project_binding
    WHERE workspace_id = NEW.workspace_id
      AND project_id = NEW.project_id
      AND state = 'active'
    LIMIT 1;

    IF target_binding_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF TG_OP = 'INSERT' OR NEW.project_id IS DISTINCT FROM OLD.project_id THEN
        INSERT INTO channel_notification_outbox (
            event_id, workspace_id, project_id, project_binding_id, issue_id,
            event_type, payload
        ) VALUES (
            gen_random_uuid(), NEW.workspace_id, NEW.project_id, target_binding_id, NEW.id,
            'issue_created',
            jsonb_build_object(
                'issue_id', NEW.id,
                'number', NEW.number,
                'title', NEW.title,
                'status', NEW.status,
                'assignee_type', NEW.assignee_type,
                'assignee_id', NEW.assignee_id,
                'creator_type', NEW.creator_type,
                'creator_id', NEW.creator_id,
                'occurred_at', now()
            )
        );
    END IF;

    IF TG_OP = 'UPDATE' AND NEW.status IS DISTINCT FROM OLD.status THEN
        INSERT INTO channel_notification_outbox (
            event_id, workspace_id, project_id, project_binding_id, issue_id,
            event_type, payload
        ) VALUES (
            gen_random_uuid(), NEW.workspace_id, NEW.project_id, target_binding_id, NEW.id,
            'issue_status_changed',
            jsonb_build_object(
                'issue_id', NEW.id,
                'number', NEW.number,
                'title', NEW.title,
                'previous_status', OLD.status,
                'status', NEW.status,
                'occurred_at', now()
            )
        );
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_channel_issue_created_notification
AFTER INSERT ON issue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_issue_notification();

CREATE TRIGGER trg_channel_issue_updated_notification
AFTER UPDATE OF status, project_id ON issue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_issue_notification();

CREATE OR REPLACE FUNCTION enqueue_channel_task_notification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_issue issue%ROWTYPE;
    target_binding_id UUID;
    notification_type TEXT;
BEGIN
    IF NEW.issue_id IS NULL
       OR NEW.status IS NOT DISTINCT FROM OLD.status
       OR NEW.status NOT IN ('failed', 'cancelled') THEN
        RETURN NEW;
    END IF;

    SELECT scoped_issue.*
    INTO target_issue
    FROM issue AS scoped_issue
    JOIN agent AS scoped_agent
      ON scoped_agent.id = NEW.agent_id
     AND scoped_agent.workspace_id = scoped_issue.workspace_id
    WHERE scoped_issue.id = NEW.issue_id;

    IF target_issue.id IS NULL OR target_issue.project_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT id
    INTO target_binding_id
    FROM channel_project_binding
    WHERE workspace_id = target_issue.workspace_id
      AND project_id = target_issue.project_id
      AND state = 'active'
    LIMIT 1;

    IF target_binding_id IS NULL THEN
        RETURN NEW;
    END IF;

    notification_type := CASE WHEN NEW.status = 'failed' THEN 'task_failed' ELSE 'task_cancelled' END;

    INSERT INTO channel_notification_outbox (
        event_id, workspace_id, project_id, project_binding_id, issue_id, task_id,
        event_type, payload
    ) VALUES (
        gen_random_uuid(), target_issue.workspace_id, target_issue.project_id,
        target_binding_id, target_issue.id, NEW.id, notification_type,
        jsonb_build_object(
            'issue_id', target_issue.id,
            'number', target_issue.number,
            'title', target_issue.title,
            'issue_status', target_issue.status,
            'task_id', NEW.id,
            'agent_id', NEW.agent_id,
            'reason', CASE
                WHEN NEW.status = 'failed' THEN 'Task execution failed'
                ELSE 'Task execution was stopped'
            END,
            'occurred_at', now()
        )
    );

    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_channel_task_notification
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_task_notification();
