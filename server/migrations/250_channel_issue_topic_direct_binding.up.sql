ALTER TABLE channel_issue_topic_binding
    ADD COLUMN installation_id UUID;

UPDATE channel_issue_topic_binding citb
SET installation_id = cpb.installation_id
FROM channel_project_binding cpb
WHERE cpb.id = citb.project_binding_id
  AND cpb.workspace_id = citb.workspace_id;

ALTER TABLE channel_issue_topic_binding
    ALTER COLUMN installation_id SET NOT NULL,
    ALTER COLUMN project_binding_id DROP NOT NULL,
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE channel_issue_topic_binding
    DROP CONSTRAINT channel_issue_topic_binding_state_check,
    ADD CONSTRAINT channel_issue_topic_binding_state_check
        CHECK (state IN (
            'active', 'manual_unbound', 'project_unbound', 'orphaned',
            'replaced', 'bot_revoked'
        ));

ALTER TABLE channel_notification_outbox
    ADD COLUMN issue_topic_binding_id UUID,
    ALTER COLUMN project_id DROP NOT NULL;

ALTER TABLE channel_notification_outbox
    ADD CONSTRAINT channel_notification_outbox_route_check
    CHECK (project_binding_id IS NOT NULL OR issue_topic_binding_id IS NOT NULL);

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
