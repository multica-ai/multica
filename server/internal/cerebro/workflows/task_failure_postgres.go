package workflows

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type PostgresTaskFailureContextStore struct {
	db cerebrodb.DBTX
}

func NewPostgresTaskFailureContextStore(db cerebrodb.DBTX) *PostgresTaskFailureContextStore {
	return &PostgresTaskFailureContextStore{db: db}
}

// WorkflowHooksEnabledForTask reports whether the task's workspace still runs
// the Workflow failure gate, per the cerebro_workflow_hooks flag.
func (s *PostgresTaskFailureContextStore) WorkflowHooksEnabledForTask(ctx context.Context, taskID pgtype.UUID) (bool, error) {
	return workflowHooksFlagForTask(ctx, s.db, taskID)
}

func (s *PostgresTaskFailureContextStore) LoadTaskFailureContext(ctx context.Context, taskID pgtype.UUID) (TaskCompletionContext, error) {
	var task, workspace, issue, agent pgtype.UUID
	var projectID, workflowID, model string
	var sessionID pgtype.Text
	err := s.db.QueryRow(ctx, `
SELECT t.id, COALESCE(i.workspace_id, a.workspace_id), t.issue_id, t.agent_id,
       COALESCE(t.context->>'project_id',''), COALESCE(t.context->>'workflow_id',''),
       COALESCE(t.model_override, a.model, ''), t.session_id
FROM agent_task_queue t
JOIN agent a ON a.id=t.agent_id
LEFT JOIN issue i ON i.id=t.issue_id
WHERE t.id=$1`, taskID).Scan(
		&task, &workspace, &issue, &agent, &projectID, &workflowID, &model, &sessionID,
	)
	if err != nil {
		return TaskCompletionContext{}, err
	}
	return TaskCompletionContext{
		TaskID: util.UUIDToString(task), WorkspaceID: util.UUIDToString(workspace),
		ProjectID: projectID, WorkflowID: workflowID, IssueID: util.UUIDToString(issue),
		AgentID: util.UUIDToString(agent), Model: model, SessionID: sessionID.String,
	}, nil
}

var _ TaskFailureContextStore = (*PostgresTaskFailureContextStore)(nil)
