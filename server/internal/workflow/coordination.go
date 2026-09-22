package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
)

const (
	workflowJobLease   = 30 * time.Second
	workflowJobBatch   = 100
	workflowJobPoll    = 3 * time.Second
	workflowOutboxWait = 30 * time.Second
)

var errWorkflowLeaseLost = errors.New("workflow scheduler lease lost")

type workflowJob struct {
	ID          string
	WorkspaceID string
	WorkflowID  string
	RunID       string
	Fence       int64
}

type workflowLease struct {
	ID    string
	Owner string
	Fence int64
}

type workflowOutboxLease struct {
	ID              string
	EventID         string
	WorkspaceID     string
	DeliveryAttempt int
}

// ensureActiveRunJobs is the restart safety net. It only creates jobs that are
// absent; an existing pending or leased job remains the source of scheduling
// truth and is never reset by a scan.
func (s *Service) ensureActiveRunJobs(ctx context.Context) error {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
SELECT r.id::text, r.workspace_id::text, r.workflow_id::text, r.body
FROM workflow_run r
LEFT JOIN workflow_job j ON j.run_id=r.id AND j.workspace_id=r.workspace_id
WHERE r.status IN ('queued','running','waiting','retry_wait','blocked')
  AND j.id IS NULL
ORDER BY r.updated_at, r.id
LIMIT 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var runID, workspaceID, workflowID string
		var raw []byte
		if err := rows.Scan(&runID, &workspaceID, &workflowID, &raw); err != nil {
			return err
		}
		var run Run
		if err := json.Unmarshal(raw, &run); err != nil {
			return err
		}
		run.ID, run.WorkspaceID, run.WorkflowID = runID, workspaceID, workflowID
		if _, err := tx.Exec(ctx, `
INSERT INTO workflow_job(id,workspace_id,run_id,kind,logical_key,due_at,status,payload_ref)
VALUES($1,$2,$3,'run.advance',$4,$5,'pending',$6)
ON CONFLICT (workspace_id,logical_key) DO NOTHING`, id(), workspaceID, runID, workflowLogicalKey(runID), nextRunDue(run), body(map[string]any{"workflow_id": workflowID})); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func workflowLogicalKey(runID string) string {
	return "run:" + runID
}

// ensureRunJob is called in the same transaction that changes a run. This
// makes a committed state transition and its next durable wake-up atomic.
func (s *Service) ensureRunJob(ctx context.Context, tx pgx.Tx, workspaceID, runID string, dueAt time.Time) error {
	var status string
	if err := tx.QueryRow(ctx, "SELECT status FROM workflow_run WHERE id=$1 AND workspace_id=$2", runID, workspaceID).Scan(&status); err != nil {
		return err
	}
	if terminal(status) {
		_, err := tx.Exec(ctx, `
UPDATE workflow_job
SET status='done', lease_owner=NULL, lease_until=NULL, updated_at=now()
WHERE workspace_id=$1 AND run_id=$2 AND status<>'done'`, workspaceID, runID)
		return err
	}
	_, err := tx.Exec(ctx, `
INSERT INTO workflow_job(id,workspace_id,run_id,kind,logical_key,due_at,status,payload_ref)
VALUES($1,$2,$3,'run.advance',$4,$5,'pending',$6)
ON CONFLICT (workspace_id,logical_key) DO UPDATE SET
  due_at=CASE WHEN workflow_job.status IN ('pending','retry_wait') THEN LEAST(workflow_job.due_at,EXCLUDED.due_at) ELSE workflow_job.due_at END,
  status=CASE WHEN workflow_job.status='done' THEN 'pending' ELSE workflow_job.status END,
  lease_owner=CASE WHEN workflow_job.status='done' THEN NULL ELSE workflow_job.lease_owner END,
  lease_until=CASE WHEN workflow_job.status='done' THEN NULL ELSE workflow_job.lease_until END,
  updated_at=now()`, id(), workspaceID, runID, workflowLogicalKey(runID), dueAt, body(map[string]any{"run_id": runID}))
	return err
}

func (s *Service) claimWorkflowJobs(ctx context.Context) ([]workflowJob, error) {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
WITH candidates AS (
  SELECT j.id,j.run_id,j.workspace_id
  FROM workflow_job j
  WHERE ((j.status IN ('pending','retry_wait') AND j.due_at<=now())
      OR (j.status='leased' AND (j.lease_until IS NULL OR j.lease_until<=now())))
    AND EXISTS (SELECT 1 FROM workflow_run r WHERE r.id=j.run_id AND r.workspace_id=j.workspace_id)
  ORDER BY j.due_at, j.id
  LIMIT 100
  FOR UPDATE SKIP LOCKED
)
UPDATE workflow_job j
SET status='leased', lease_owner=$1, lease_until=now()+$2::interval,
    fence=j.fence+1, updated_at=now()
FROM candidates c
WHERE j.id=c.id
RETURNING j.id::text,j.workspace_id::text,
  (SELECT r.workflow_id::text FROM workflow_run r WHERE r.id=j.run_id AND r.workspace_id=j.workspace_id),
  j.run_id::text,j.fence`, s.workerID, workflowJobLease.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]workflowJob, 0, workflowJobBatch)
	for rows.Next() {
		var job workflowJob
		if err := rows.Scan(&job.ID, &job.WorkspaceID, &job.WorkflowID, &job.RunID, &job.Fence); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *Service) lockWorkflowJob(ctx context.Context, tx pgx.Tx, lease workflowLease) error {
	var owner, status string
	var fence int64
	var leaseUntil pgtype.Timestamptz
	err := tx.QueryRow(ctx, `SELECT lease_owner,fence,status,lease_until FROM workflow_job WHERE id=$1 FOR UPDATE`, lease.ID).Scan(&owner, &fence, &status, &leaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return errWorkflowLeaseLost
	}
	if err != nil {
		return err
	}
	if owner != lease.Owner || fence != lease.Fence || status != "leased" || !leaseUntil.Valid || !leaseUntil.Time.After(time.Now().UTC()) {
		return errWorkflowLeaseLost
	}
	return nil
}

func (s *Service) finishWorkflowJob(ctx context.Context, tx pgx.Tx, lease workflowLease, run Run) error {
	status := "pending"
	dueAt := nextRunDue(run)
	if terminal(run.Status) {
		status = "done"
		dueAt = time.Now().UTC()
	}
	result, err := tx.Exec(ctx, `
UPDATE workflow_job
SET status=$2,due_at=$3,lease_owner=NULL,lease_until=NULL,updated_at=now()
WHERE id=$1 AND lease_owner=$4 AND fence=$5 AND status='leased' AND lease_until>now()`, lease.ID, status, dueAt, lease.Owner, lease.Fence)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errWorkflowLeaseLost
	}
	return nil
}

func nextRunDue(run Run) time.Time {
	now := time.Now().UTC()
	due := now.Add(workflowJobPoll)
	for _, node := range run.Nodes {
		if node.Status == "retry_wait" && node.RetryAt != nil && node.RetryAt.Before(due) {
			due = *node.RetryAt
		}
	}
	if run.DeadlineAt != nil && run.DeadlineAt.Before(due) {
		due = *run.DeadlineAt
	}
	return due
}

type WorkflowEvent struct {
	ID          string         `json:"id"`
	WorkspaceID string         `json:"workspace_id"`
	RunID       string         `json:"run_id"`
	Sequence    int64          `json:"sequence"`
	EventType   string         `json:"event_type"`
	ActorType   string         `json:"actor_type"`
	ActorID     string         `json:"actor_id,omitempty"`
	Payload     map[string]any `json:"payload"`
	CreatedAt   time.Time      `json:"created_at"`
}

func appendWorkflowEvent(ctx context.Context, tx pgx.Tx, run Run, eventType, actorType, actorID string, payload map[string]any) (WorkflowEvent, error) {
	var event WorkflowEvent
	var sequence int64
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(sequence),0)+1 FROM workflow_event WHERE run_id=$1", uuid(run.ID)).Scan(&sequence); err != nil {
		return event, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	payload["workflow_id"] = run.WorkflowID
	payload["run_id"] = run.ID
	payload["sequence"] = sequence
	payload["state_revision"] = run.StateRevision
	event = WorkflowEvent{ID: id(), WorkspaceID: run.WorkspaceID, RunID: run.ID, Sequence: sequence, EventType: eventType, ActorType: actorType, ActorID: actorID, Payload: payload, CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(ctx, `
INSERT INTO workflow_event(id,workspace_id,run_id,sequence,event_type,actor_type,actor_id,payload)
VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, uuid(event.ID), uuid(event.WorkspaceID), uuid(event.RunID), event.Sequence, event.EventType, event.ActorType, nullableText(event.ActorID), body(event.Payload)); err != nil {
		return WorkflowEvent{}, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO workflow_outbox(id,workspace_id,event_id,channel,recipient_id)
VALUES($1,$2,$3,'workflow',$4)
ON CONFLICT (event_id,channel,recipient_id) DO NOTHING`, uuid(id()), uuid(event.WorkspaceID), uuid(event.ID), "workspace:"+event.WorkspaceID); err != nil {
		return WorkflowEvent{}, err
	}
	return event, nil
}

func (s *Service) processWorkflowOutbox(ctx context.Context) error {
	tx, err := s.Tx.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `
SELECT id::text,event_id::text,workspace_id::text,delivery_attempt
FROM workflow_outbox
WHERE delivered_at IS NULL AND due_at<=now()
ORDER BY due_at,id
LIMIT 100
FOR UPDATE SKIP LOCKED`)
	if err != nil {
		return err
	}
	leases := make([]workflowOutboxLease, 0, 100)
	for rows.Next() {
		var item workflowOutboxLease
		if err := rows.Scan(&item.ID, &item.EventID, &item.WorkspaceID, &item.DeliveryAttempt); err != nil {
			rows.Close()
			return err
		}
		item.DeliveryAttempt++
		if _, err := tx.Exec(ctx, `UPDATE workflow_outbox SET delivery_attempt=$2,due_at=now()+$3::interval,updated_at=now() WHERE id=$1`, item.ID, item.DeliveryAttempt, workflowOutboxWait.String()); err != nil {
			rows.Close()
			return err
		}
		leases = append(leases, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, item := range leases {
		var event WorkflowEvent
		var payload []byte
		var actorID *string
		err := s.DB.QueryRow(ctx, `
SELECT id::text,workspace_id::text,run_id::text,sequence,event_type,actor_type,actor_id,payload,created_at
FROM workflow_event WHERE id=$1`, uuid(item.EventID)).Scan(&event.ID, &event.WorkspaceID, &event.RunID, &event.Sequence, &event.EventType, &event.ActorType, &actorID, &payload, &event.CreatedAt)
		if err == nil {
			event.ActorID = stringValue(actorID)
			err = json.Unmarshal(payload, &event.Payload)
		}
		if err != nil {
			_, _ = s.DB.Exec(ctx, `UPDATE workflow_outbox SET last_error=$2,due_at=now()+$3::interval,updated_at=now() WHERE id=$1 AND delivered_at IS NULL AND delivery_attempt=$4`, item.ID, err.Error(), workflowOutboxWait.String(), item.DeliveryAttempt)
			continue
		}
		if s.Bus != nil {
			s.Bus.Publish(events.Event{Type: event.EventType, WorkspaceID: event.WorkspaceID, ActorType: event.ActorType, ActorID: event.ActorID, Payload: event.Payload})
		}
		_, _ = s.DB.Exec(ctx, `UPDATE workflow_outbox SET delivered_at=now(),last_error=NULL,updated_at=now() WHERE id=$1 AND delivered_at IS NULL AND delivery_attempt=$2`, item.ID, item.DeliveryAttempt)
	}
	return nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s *Service) recordRunMutation(ctx context.Context, tx pgx.Tx, run Run, eventType, actorType, actorID string) error {
	if err := s.ensureRunJob(ctx, tx, run.WorkspaceID, run.ID, nextRunDue(run)); err != nil {
		return err
	}
	_, err := appendWorkflowEvent(ctx, tx, run, eventType, actorType, actorID, map[string]any{
		"status":         run.Status,
		"state_revision": run.StateRevision,
		"reason_code":    run.ReasonCode,
	})
	return err
}

func (s *Service) ListRunEvents(ctx context.Context, ws, wid, rid string, afterSequence int64, limit int) ([]WorkflowEvent, int64, error) {
	if _, err := s.GetRun(ctx, ws, wid, rid); err != nil {
		return nil, afterSequence, err
	}
	if afterSequence < 0 {
		afterSequence = 0
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.DB.Query(ctx, `
SELECT id::text,workspace_id::text,run_id::text,sequence,event_type,actor_type,actor_id,payload,created_at
FROM workflow_event
WHERE workspace_id=$1 AND run_id=$2 AND sequence>$3
ORDER BY sequence
LIMIT $4`, ws, rid, afterSequence, limit)
	if err != nil {
		return nil, afterSequence, err
	}
	defer rows.Close()
	eventsList := make([]WorkflowEvent, 0)
	next := afterSequence
	for rows.Next() {
		var event WorkflowEvent
		var actorID *string
		var payload []byte
		if err := rows.Scan(&event.ID, &event.WorkspaceID, &event.RunID, &event.Sequence, &event.EventType, &event.ActorType, &actorID, &payload, &event.CreatedAt); err != nil {
			return nil, afterSequence, err
		}
		event.ActorID = stringValue(actorID)
		if err := json.Unmarshal(payload, &event.Payload); err != nil {
			return nil, afterSequence, fmt.Errorf("decode workflow event %s: %w", event.ID, err)
		}
		eventsList = append(eventsList, event)
		next = event.Sequence
	}
	return eventsList, next, rows.Err()
}
