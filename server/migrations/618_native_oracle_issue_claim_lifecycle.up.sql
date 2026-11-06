-- Native unfinished-Issue events: atomic claim exclusion and lifecycle cleanup.
-- This changes ONLY the subset of claim paths involving opt-in oracle_issue_event
-- tasks. Normal cross-Agent collaboration without an event Task remains valid.
-- PRECONDITION: migration 615 created the partial active-event index.

CREATE OR REPLACE FUNCTION native_oracle_issue_claim_guard()
RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 event_task boolean := COALESCE((NEW.context->>'oracle_issue_event')='true',false);
 effective_status text;
 event_enabled boolean;
BEGIN
 IF NEW.issue_id IS NULL OR OLD.status<>'queued' OR NEW.status<>'dispatched' THEN
  RETURN NEW;
 END IF;
 -- Legacy non-event claims are untouched if this Issue has no active native event.
 IF NOT event_task AND NOT EXISTS (
   SELECT 1 FROM agent_task_queue t
   WHERE t.issue_id=NEW.issue_id AND t.context->>'oracle_issue_event'='true'
   AND t.status IN ('queued','deferred','dispatched','running','waiting_local_directory')
 ) THEN RETURN NEW; END IF;

 -- Every Task claim with an event participant serializes on the same Issue.
 -- The original daemon-wide and per-Agent capacity/Lease fences stay intact.
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.issue_id::text,2746));

 IF event_task THEN
  SELECT issue_effective_status(i.workspace_id,i.status),
    COALESCE((w.settings->'oracle_issue_events'->>'enabled')::boolean,false)
  INTO effective_status,event_enabled
  FROM issue i JOIN workspace w ON w.id=i.workspace_id
  WHERE i.id=NEW.issue_id;
  IF effective_status IS NULL OR effective_status IN ('done','cancelled')
    OR NOT COALESCE(event_enabled,false)
  THEN RETURN NULL; END IF;
 END IF;

 IF EXISTS (
  SELECT 1 FROM agent_task_queue t
  WHERE t.issue_id=NEW.issue_id AND t.id<>NEW.id
    AND t.status IN ('dispatched','running','waiting_local_directory')
    AND (event_task OR t.context->>'oracle_issue_event'='true')
 ) THEN RETURN NULL; END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS native_oracle_issue_claim_guard ON agent_task_queue;
CREATE TRIGGER native_oracle_issue_claim_guard
BEFORE UPDATE OF status ON agent_task_queue
FOR EACH ROW WHEN (OLD.status='queued' AND NEW.status='dispatched')
EXECUTE FUNCTION native_oracle_issue_claim_guard();

-- Issue-close cleanup follows migration 533's existing application transaction
-- (StopClosedIssueWakeups -> CancelUnstartedIssueWakeupTasks), NOT a
-- deprecated database trigger. Its SQL query is extended in this patch.

-- Revoking or editing the owner config atomically cancels old unstarted
-- event tasks; a new event must pass the new exact Agent/Runtime binding.
CREATE OR REPLACE FUNCTION cancel_oracle_event_tasks_on_workspace_change()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.settings->'oracle_issue_events')
       IS DISTINCT FROM (NEW.settings->'oracle_issue_events') THEN
  UPDATE agent_task_queue t
  SET status='cancelled',completed_at=now(),
      error='Oracle Issue event workspace config changed'
  FROM agent a
  WHERE t.agent_id=a.id AND a.workspace_id=NEW.id
    AND t.context->>'oracle_issue_event'='true'
    AND t.status IN ('queued','deferred','dispatched')
    AND t.started_at IS NULL;
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER IF EXISTS cancel_oracle_event_tasks_on_workspace_change ON workspace;
CREATE TRIGGER cancel_oracle_event_tasks_on_workspace_change
AFTER UPDATE OF settings ON workspace
FOR EACH ROW EXECUTE FUNCTION cancel_oracle_event_tasks_on_workspace_change();
