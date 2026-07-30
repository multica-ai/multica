DELETE FROM channel_notification_outbox
WHERE event_type IN (
    'comment_created',
    'comment_updated',
    'task_started',
    'task_completed',
    'completed',
    'task_result',
    'assignee_changed',
    'priority_changed',
    'blocked_reason_changed'
);

DROP TRIGGER IF EXISTS trg_channel_comment_created_notification ON comment;
DROP TRIGGER IF EXISTS trg_channel_comment_updated_notification ON comment;
DROP FUNCTION IF EXISTS enqueue_channel_comment_notification();

ALTER TABLE channel_notification_outbox
    DROP CONSTRAINT IF EXISTS channel_notification_outbox_event_type_check,
    ADD CONSTRAINT channel_notification_outbox_event_type_check
        CHECK (event_type IN (
            'issue_created',
            'issue_status_changed',
            'task_failed',
            'task_cancelled'
        ));

CREATE OR REPLACE FUNCTION enqueue_channel_issue_notification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_project_binding_id UUID;
    target_topic_binding_id UUID;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.project_id IS DISTINCT FROM OLD.project_id THEN
        UPDATE channel_issue_topic_binding
        SET state = 'project_unbound',
            unbound_at = now(),
            updated_at = now()
        WHERE workspace_id = NEW.workspace_id
          AND issue_id = NEW.id
          AND project_binding_id IS NOT NULL
          AND project_id IS DISTINCT FROM NEW.project_id
          AND state = 'active';
    END IF;

    SELECT id, project_binding_id
    INTO target_topic_binding_id, target_project_binding_id
    FROM channel_issue_topic_binding
    WHERE workspace_id = NEW.workspace_id
      AND issue_id = NEW.id
      AND state = 'active'
    LIMIT 1;

    IF (TG_OP = 'INSERT' OR NEW.project_id IS DISTINCT FROM OLD.project_id)
       AND target_topic_binding_id IS NULL
       AND NEW.project_id IS NOT NULL THEN
        SELECT id
        INTO target_project_binding_id
        FROM channel_project_binding
        WHERE workspace_id = NEW.workspace_id
          AND project_id = NEW.project_id
          AND state = 'active'
        LIMIT 1;

        IF target_project_binding_id IS NOT NULL THEN
            INSERT INTO channel_notification_outbox (
                event_id, workspace_id, project_id, project_binding_id,
                issue_topic_binding_id, issue_id, event_type, payload
            ) VALUES (
                gen_random_uuid(), NEW.workspace_id, NEW.project_id,
                target_project_binding_id, NULL, NEW.id, 'issue_created',
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
    END IF;

    IF TG_OP = 'UPDATE' AND NEW.status IS DISTINCT FROM OLD.status THEN
        IF target_topic_binding_id IS NULL AND NEW.project_id IS NOT NULL THEN
            SELECT id
            INTO target_project_binding_id
            FROM channel_project_binding
            WHERE workspace_id = NEW.workspace_id
              AND project_id = NEW.project_id
              AND state = 'active'
            LIMIT 1;
        END IF;

        IF target_topic_binding_id IS NOT NULL OR target_project_binding_id IS NOT NULL THEN
            INSERT INTO channel_notification_outbox (
                event_id, workspace_id, project_id, project_binding_id,
                issue_topic_binding_id, issue_id, event_type, payload
            ) VALUES (
                gen_random_uuid(), NEW.workspace_id, NEW.project_id,
                target_project_binding_id, target_topic_binding_id, NEW.id,
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
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_channel_issue_updated_notification ON issue;
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
    target_project_binding_id UUID;
    target_topic_binding_id UUID;
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

    IF target_issue.id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT id, project_binding_id
    INTO target_topic_binding_id, target_project_binding_id
    FROM channel_issue_topic_binding
    WHERE workspace_id = target_issue.workspace_id
      AND issue_id = target_issue.id
      AND state = 'active'
    LIMIT 1;

    IF target_topic_binding_id IS NULL AND target_issue.project_id IS NOT NULL THEN
        SELECT id
        INTO target_project_binding_id
        FROM channel_project_binding
        WHERE workspace_id = target_issue.workspace_id
          AND project_id = target_issue.project_id
          AND state = 'active'
        LIMIT 1;
    END IF;

    IF target_topic_binding_id IS NULL AND target_project_binding_id IS NULL THEN
        RETURN NEW;
    END IF;

    notification_type := CASE WHEN NEW.status = 'failed' THEN 'task_failed' ELSE 'task_cancelled' END;

    INSERT INTO channel_notification_outbox (
        event_id, workspace_id, project_id, project_binding_id,
        issue_topic_binding_id, issue_id, task_id, event_type, payload
    ) VALUES (
        gen_random_uuid(), target_issue.workspace_id, target_issue.project_id,
        target_project_binding_id, target_topic_binding_id,
        target_issue.id, NEW.id, notification_type,
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

DROP TRIGGER IF EXISTS trg_channel_task_notification ON agent_task_queue;
CREATE TRIGGER trg_channel_task_notification
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_task_notification();

ALTER TABLE channel_notification_outbox
    DROP COLUMN IF EXISTS event_order;
