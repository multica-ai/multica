package workflows

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

type PostgresTaskFailureStore struct {
	db cerebrodb.DBTX
}

func NewPostgresTaskFailureStore(db cerebrodb.DBTX) *PostgresTaskFailureStore {
	return &PostgresTaskFailureStore{db: db}
}

// LoadTaskFailureContext resolves the failed task's issue/workspace context and
// whether a retry child is already queued. The inner JOIN on issue makes
// chat-session tasks resolve to pgx.ErrNoRows, which the gate treats as skip.
func (s *PostgresTaskFailureStore) LoadTaskFailureContext(ctx context.Context, taskID pgtype.UUID) (TaskFailureContext, error) {
	var task, workspace, issue, agent pgtype.UUID
	var projectID, issueStatus, model, failureReason, errorText string
	var sessionID pgtype.Text
	var attempt, maxAttempts int32
	var retryPending bool
	err := s.db.QueryRow(ctx, `
SELECT t.id, i.workspace_id, t.issue_id, t.agent_id,
       COALESCE(t.context->>'project_id',''),
       i.status, COALESCE(t.model_override, a.model, ''), t.session_id,
       t.attempt, t.max_attempts,
       COALESCE(t.failure_reason,''), COALESCE(t.error,''),
       EXISTS(
         SELECT 1 FROM agent_task_queue c
         WHERE c.parent_task_id = t.id
           AND c.status NOT IN ('failed','cancelled','completed')
       )
FROM agent_task_queue t
JOIN agent a ON a.id = t.agent_id
JOIN issue i ON i.id = t.issue_id
WHERE t.id = $1`, taskID).Scan(
		&task, &workspace, &issue, &agent, &projectID,
		&issueStatus, &model, &sessionID, &attempt, &maxAttempts,
		&failureReason, &errorText, &retryPending,
	)
	if err != nil {
		return TaskFailureContext{}, err
	}
	enabled := false
	flags, err := cerebrodb.New(s.db).ListCerebroWorkspaceFeatureFlags(ctx, workspace)
	if err != nil {
		return TaskFailureContext{}, err
	}
	for _, flag := range flags {
		if flag.FlagKey == "cerebro_workflow_hooks" {
			enabled = flag.Enabled
		}
	}
	return TaskFailureContext{
		HooksEnabled: enabled,
		TaskID:       util.UUIDToString(task), WorkspaceID: util.UUIDToString(workspace),
		ProjectID: projectID, IssueID: util.UUIDToString(issue), IssueStatus: issueStatus,
		AgentID: util.UUIDToString(agent), Model: model, SessionID: sessionID.String,
		FailureReason: failureReason, ErrorText: errorText,
		Attempt: int(attempt), MaxAttempts: int(maxAttempts),
		RetryPending: retryPending,
	}, nil
}
