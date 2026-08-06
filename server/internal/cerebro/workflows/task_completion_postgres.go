package workflows

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type PostgresTaskCompletionStore struct {
	db cerebrodb.DBTX
}

func NewPostgresTaskCompletionStore(db cerebrodb.DBTX) *PostgresTaskCompletionStore {
	return &PostgresTaskCompletionStore{db: db}
}

// WorkflowHooksEnabled reports whether the task's workspace still runs the
// Workflow completion gate, per the cerebro_workflow_hooks flag.
func (s *PostgresTaskCompletionStore) WorkflowHooksEnabled(ctx context.Context, taskID pgtype.UUID) (bool, error) {
	return workflowHooksFlagForTask(ctx, s.db, taskID)
}

func (s *PostgresTaskCompletionStore) LoadTaskCompletionContext(ctx context.Context, taskID pgtype.UUID) (TaskCompletionContext, error) {
	var task, workspace, issue, agent pgtype.UUID
	var projectID, workflowID, issueStatus, model string
	var sessionID pgtype.Text
	var attempt int32
	var started pgtype.Timestamptz
	err := s.db.QueryRow(ctx, `
SELECT t.id, COALESCE(i.workspace_id, a.workspace_id), t.issue_id, t.agent_id,
       COALESCE(t.context->>'project_id',''), COALESCE(t.context->>'workflow_id',''),
       COALESCE(i.status, ''), COALESCE(t.model_override, a.model, ''), t.session_id,
       t.attempt, COALESCE(t.started_at, t.created_at)
FROM agent_task_queue t
JOIN agent a ON a.id=t.agent_id
LEFT JOIN issue i ON i.id=t.issue_id
WHERE t.id=$1`, taskID).Scan(
		&task, &workspace, &issue, &agent, &projectID, &workflowID,
		&issueStatus, &model, &sessionID, &attempt, &started,
	)
	if err != nil {
		return TaskCompletionContext{}, err
	}
	return TaskCompletionContext{
		TaskID: util.UUIDToString(task), WorkspaceID: util.UUIDToString(workspace),
		ProjectID: projectID, WorkflowID: workflowID, IssueID: util.UUIDToString(issue),
		IssueStatus: issueStatus, AgentID: util.UUIDToString(agent), Model: model,
		SessionID: sessionID.String, TaskAttempt: int(attempt), StartedAt: started.Time,
	}, nil
}

func (s *PostgresTaskCompletionStore) ResolveContinuation(ctx context.Context, in TaskCompletionContext) (*TaskContinuation, error) {
	taskID, err := util.ParseUUID(in.TaskID)
	if err != nil {
		return nil, err
	}
	issueID, err := util.ParseUUID(in.IssueID)
	if err != nil {
		return nil, err
	}
	agentID, err := util.ParseUUID(in.AgentID)
	if err != nil {
		return nil, err
	}

	var evidence pgtype.UUID
	var squadID pgtype.UUID
	err = s.db.QueryRow(ctx, `SELECT id, squad_id FROM agent_task_queue
WHERE source_task_id=$1 AND status <> 'cancelled' ORDER BY created_at DESC LIMIT 1`, taskID).Scan(&evidence, &squadID)
	if err == nil {
		kind := ContinuationAgent
		if squadID.Valid {
			kind = ContinuationSquad
		}
		return &TaskContinuation{Kind: kind, EvidenceID: util.UUIDToString(evidence)}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	err = s.db.QueryRow(ctx, `SELECT id FROM cerebro_agent_wakeup
WHERE issue_id=$1 AND agent_id=$2 AND created_at >= $3 AND state IN ('pending','claimed','dispatched')
ORDER BY created_at DESC LIMIT 1`, issueID, agentID, in.StartedAt).Scan(&evidence)
	if err == nil {
		return &TaskContinuation{Kind: ContinuationWakeup, EvidenceID: util.UUIDToString(evidence)}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	err = s.db.QueryRow(ctx, `SELECT id FROM cerebro_session
WHERE issue_id=$1 AND updated_at >= $2 AND handoff IS NOT NULL
ORDER BY updated_at DESC LIMIT 1`, issueID, in.StartedAt).Scan(&evidence)
	if err == nil {
		return &TaskContinuation{Kind: ContinuationHandoff, EvidenceID: util.UUIDToString(evidence)}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	var content string
	err = s.db.QueryRow(ctx, `SELECT id, content FROM comment
WHERE issue_id=$1 AND author_type='agent' AND author_id=$2 AND created_at >= $3
AND content LIKE '%mention://member/%' ORDER BY created_at DESC LIMIT 1`, issueID, agentID, in.StartedAt).Scan(&evidence, &content)
	if err == nil {
		kind := ContinuationNotifyMember
		if strings.Contains(content, "?") {
			kind = ContinuationAskMember
		}
		return &TaskContinuation{Kind: kind, EvidenceID: util.UUIDToString(evidence)}, nil
	}
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	if in.IssueStatus == "blocked" {
		return &TaskContinuation{Kind: ContinuationBlocked, EvidenceID: in.IssueID}, nil
	}
	return nil, nil
}

var _ TaskCompletionContextStore = (*PostgresTaskCompletionStore)(nil)

// Keep the timestamp dependency explicit in this file; database evidence is
// always bounded to the current task run, never inferred from older actions.
var _ = time.Time{}
