package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EnqueueTaskForWorkflowSquadLeader preserves each stage handoff even when an
// older stage or another squad already holds the leader's pending-task slot.
func (s *TaskService) EnqueueTaskForWorkflowSquadLeader(ctx context.Context, issue db.Issue, leaderID, squadID pgtype.UUID, handoffNote string, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	return s.enqueueMentionTaskWithCommentPlan(ctx, issue, leaderID, pgtype.UUID{}, nil, true, squadID, false, handoffNote, actorUserID, pgtype.UUID{}, OriginDerived, false, "workflow_squad_handoff", true)
}

func (s *TaskService) createWorkflowSquadHandoffTask(ctx context.Context, issue db.Issue, params db.CreateAgentTaskParams) (db.AgentTaskQueue, error) {
	if s.TxStarter == nil {
		return db.AgentTaskQueue{}, errors.New("workflow squad handoff requires a transaction")
	}
	// Deferred rows can coexist with an occupied queued/dispatched slot. The
	// existing promoter releases them when that slot frees; never rewrite the
	// binding, role, comments or attribution of a task already offered to a daemon.
	params.FireAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	params.WorkflowStep = pgtype.Text{String: issue.Status, Valid: true}
	params.WorkflowHandoffRevision = pgtype.Int8{Int64: issue.Revision, Valid: true}
	var task db.AgentTaskQueue
	err := s.runInTx(ctx, func(q *db.Queries) error {
		if err := q.LockWorkflowSquadHandoff(ctx, issue.ID); err != nil {
			return err
		}
		_, err := q.GetWorkflowSquadHandoffTask(ctx, db.GetWorkflowSquadHandoffTaskParams{
			IssueID: issue.ID, AgentID: params.AgentID, SquadID: params.SquadID,
			IssueRevision: issue.Revision,
		})
		if err == nil {
			return ErrDuplicatePendingTask
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		task, err = q.CreateAgentTask(ctx, params)
		return err
	})
	return task, err
}
