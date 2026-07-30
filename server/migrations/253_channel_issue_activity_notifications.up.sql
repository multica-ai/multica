ALTER TABLE channel_notification_outbox
    ADD COLUMN event_order BIGINT GENERATED ALWAYS AS IDENTITY,
    DROP CONSTRAINT IF EXISTS channel_notification_outbox_event_type_check,
    ADD CONSTRAINT channel_notification_outbox_event_type_check
        CHECK (event_type IN (
            'issue_created',
            'issue_status_changed',
            'comment_created',
            'comment_updated',
            'task_started',
            'task_completed',
            'completed',
            'task_result',
            'task_failed',
            'task_cancelled',
            'assignee_changed',
            'priority_changed',
            'blocked_reason_changed'
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

    IF TG_OP = 'UPDATE' THEN
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
            IF NEW.status IS DISTINCT FROM OLD.status THEN
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

            IF NEW.assignee_type IS DISTINCT FROM OLD.assignee_type
               OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id THEN
                INSERT INTO channel_notification_outbox (
                    event_id, workspace_id, project_id, project_binding_id,
                    issue_topic_binding_id, issue_id, event_type, payload
                ) VALUES (
                    gen_random_uuid(), NEW.workspace_id, NEW.project_id,
                    target_project_binding_id, target_topic_binding_id, NEW.id,
                    'assignee_changed',
                    jsonb_build_object(
                        'issue_id', NEW.id,
                        'number', NEW.number,
                        'title', NEW.title,
                        'previous_assignee_type', OLD.assignee_type,
                        'previous_assignee_id', OLD.assignee_id,
                        'assignee_type', NEW.assignee_type,
                        'assignee_id', NEW.assignee_id,
                        'occurred_at', now()
                    )
                );
            END IF;

            IF NEW.priority IS DISTINCT FROM OLD.priority THEN
                INSERT INTO channel_notification_outbox (
                    event_id, workspace_id, project_id, project_binding_id,
                    issue_topic_binding_id, issue_id, event_type, payload
                ) VALUES (
                    gen_random_uuid(), NEW.workspace_id, NEW.project_id,
                    target_project_binding_id, target_topic_binding_id, NEW.id,
                    'priority_changed',
                    jsonb_build_object(
                        'issue_id', NEW.id,
                        'number', NEW.number,
                        'title', NEW.title,
                        'previous_priority', OLD.priority,
                        'priority', NEW.priority,
                        'occurred_at', now()
                    )
                );
            END IF;

            IF (NEW.metadata -> 'blocked_reason') IS DISTINCT FROM
               (OLD.metadata -> 'blocked_reason') THEN
                INSERT INTO channel_notification_outbox (
                    event_id, workspace_id, project_id, project_binding_id,
                    issue_topic_binding_id, issue_id, event_type, payload
                ) VALUES (
                    gen_random_uuid(), NEW.workspace_id, NEW.project_id,
                    target_project_binding_id, target_topic_binding_id, NEW.id,
                    'blocked_reason_changed',
                    jsonb_build_object(
                        'issue_id', NEW.id,
                        'number', NEW.number,
                        'title', NEW.title,
                        'previous_blocked_reason', OLD.metadata -> 'blocked_reason',
                        'blocked_reason', NEW.metadata -> 'blocked_reason',
                        'occurred_at', now()
                    )
                );
            END IF;
        END IF;
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_channel_issue_updated_notification ON issue;
CREATE TRIGGER trg_channel_issue_updated_notification
AFTER UPDATE OF status, project_id, assignee_type, assignee_id, priority, metadata ON issue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_issue_notification();

CREATE OR REPLACE FUNCTION enqueue_channel_comment_notification()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
DECLARE
    target_issue issue%ROWTYPE;
    target_project_binding_id UUID;
    target_topic_binding_id UUID;
    notification_type TEXT;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.content IS NOT DISTINCT FROM OLD.content THEN
        RETURN NEW;
    END IF;

    SELECT scoped_issue.*
    INTO target_issue
    FROM issue AS scoped_issue
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

    notification_type := CASE
        WHEN TG_OP = 'INSERT' THEN 'comment_created'
        ELSE 'comment_updated'
    END;

    INSERT INTO channel_notification_outbox (
        event_id, workspace_id, project_id, project_binding_id,
        issue_topic_binding_id, issue_id, event_type, payload
    ) VALUES (
        gen_random_uuid(), target_issue.workspace_id, target_issue.project_id,
        target_project_binding_id, target_topic_binding_id,
        target_issue.id, notification_type,
        jsonb_build_object(
            'issue_id', target_issue.id,
            'number', target_issue.number,
            'title', target_issue.title,
            'comment_id', NEW.id,
            'comment_type', NEW.type,
            'author_type', NEW.author_type,
            'author_id', NEW.author_id,
            'content', NEW.content,
            'previous_content', CASE WHEN TG_OP = 'UPDATE' THEN OLD.content ELSE NULL END,
            'occurred_at', now()
        )
    );

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_channel_comment_created_notification ON comment;
CREATE TRIGGER trg_channel_comment_created_notification
AFTER INSERT ON comment
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_comment_notification();

DROP TRIGGER IF EXISTS trg_channel_comment_updated_notification ON comment;
CREATE TRIGGER trg_channel_comment_updated_notification
AFTER UPDATE OF content ON comment
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_comment_notification();

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
    IF NEW.issue_id IS NULL THEN
        RETURN NEW;
    END IF;

    IF NEW.status IS NOT DISTINCT FROM OLD.status
       AND NEW.result IS NOT DISTINCT FROM OLD.result THEN
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

    IF NEW.status IS DISTINCT FROM OLD.status
       AND NEW.status IN ('running', 'completed', 'failed', 'cancelled') THEN
        notification_type := CASE NEW.status
            WHEN 'running' THEN 'task_started'
            WHEN 'completed' THEN 'task_completed'
            WHEN 'failed' THEN 'task_failed'
            ELSE 'task_cancelled'
        END;

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
                'task_status', NEW.status,
                'reason', CASE
                    WHEN NEW.status = 'failed' THEN COALESCE(NULLIF(NEW.error, ''), 'Task execution failed')
                    WHEN NEW.status = 'cancelled' THEN 'Task execution was stopped'
                    ELSE NULL
                END,
                'occurred_at', now()
            )
        );
    END IF;

    IF NEW.result IS DISTINCT FROM OLD.result AND NEW.result IS NOT NULL THEN
        INSERT INTO channel_notification_outbox (
            event_id, workspace_id, project_id, project_binding_id,
            issue_topic_binding_id, issue_id, task_id, event_type, payload
        ) VALUES (
            gen_random_uuid(), target_issue.workspace_id, target_issue.project_id,
            target_project_binding_id, target_topic_binding_id,
            target_issue.id, NEW.id, 'task_result',
            jsonb_build_object(
                'issue_id', target_issue.id,
                'number', target_issue.number,
                'title', target_issue.title,
                'issue_status', target_issue.status,
                'task_id', NEW.id,
                'agent_id', NEW.agent_id,
                'task_status', NEW.status,
                'result', NEW.result,
                'occurred_at', now()
            )
        );
    END IF;

    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_channel_task_notification ON agent_task_queue;
CREATE TRIGGER trg_channel_task_notification
AFTER UPDATE OF status, result ON agent_task_queue
FOR EACH ROW
EXECUTE FUNCTION enqueue_channel_task_notification();
