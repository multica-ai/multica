package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HasSquadLeaderNoActionEvaluationForTask reports whether this exact task
// already recorded a squad leader no_action evaluation.
func HasSquadLeaderNoActionEvaluationForTask(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) (bool, error) {
	if q == nil || !task.ID.Valid || !task.IssueID.Valid || !task.AgentID.Valid {
		return false, nil
	}
	return q.HasSquadLeaderNoActionEvaluationForTask(ctx, db.HasSquadLeaderNoActionEvaluationForTaskParams{
		IssueID: task.IssueID,
		AgentID: task.AgentID,
		TaskID:  util.UUIDToString(task.ID),
	})
}

// RegisterSquadCompletionWakeup pairs a no_action decision with a durable wait
// for the workers whose replies caused this leader turn. The caller's transaction
// also records the evaluation; neither may succeed without the other.
func (s *IssueWakeupService) RegisterSquadCompletionWakeup(ctx context.Context, q *db.Queries, task db.AgentTaskQueue) error {
	if !task.IsLeaderTask || !task.SquadID.Valid || !task.OriginatorUserID.Valid {
		return nil
	}
	issue, err := q.LockWakeupIssue(ctx, task.IssueID)
	if err != nil {
		return err
	}
	// The evaluation is the durable retry guard, even after a person deletes
	// the rule. It commits in the same transaction as the completion waits.
	if recorded, err := HasSquadLeaderNoActionEvaluationForTask(ctx, q, task); err != nil {
		return err
	} else if recorded {
		return nil
	}
	task, err = q.LockWakeupSourceTask(ctx, db.LockWakeupSourceTaskParams{ID: task.ID, IssueID: issue.ID})
	if err != nil {
		return err
	}
	if task.Status != "running" {
		return nil
	}
	workers, err := q.ListSquadCompletionWorkers(ctx, db.ListSquadCompletionWorkersParams{
		IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, AgentID: task.AgentID, SquadID: task.SquadID,
		CommentIds: task.DeliveredCommentIds,
	})
	if err != nil {
		return err
	}
	if len(workers) == 0 {
		return nil
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: task.AgentID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return err
	}
	if err = s.authorize(ctx, q, issue.WorkspaceID, task.OriginatorUserID, agent); err != nil {
		return err
	}
	for _, worker := range workers {
		id := squadCompletionWakeupID(task.AgentID, worker.ID)
		if _, err = q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: id, WorkspaceID: issue.WorkspaceID}); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		wake, err := q.CreateIssueWakeup(ctx, db.CreateIssueWakeupParams{
			ID: id, WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: task.AgentID, CreatedBy: task.OriginatorUserID, SourceTaskID: task.ID,
			Instruction: "The delegated worker run has ended. Review its result and the latest human instructions, then decide whether remaining authorized work needs coordination. Respect pauses and restrictions; do not repeat work already delivered or queued.",
			Kind:        "event", Mode: "once", EventTypes: []string{"task.completed", "task.failed", "task.cancelled"}, FilterTaskID: worker.ID, Timezone: "UTC",
		})
		if err != nil {
			return err
		}
		if _, err = squadCompletionWakeupLeader(ctx, q, wake); err != nil {
			return err
		}
		if err = recordTaskWakeupSnapshot(ctx, q, &wake, worker, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

func squadCompletionWakeupID(agentID, workerID pgtype.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: uuid.NewSHA1(uuid.UUID(agentID.Bytes), workerID.Bytes[:]), Valid: true}
}

// Automatic waits have a stable ID per leader and worker, across evaluations.
// Preserve the squad role through both dispatch and joining an existing run.
func squadCompletionWakeupLeader(ctx context.Context, q *db.Queries, w db.IssueWakeup) (db.AgentTaskQueue, error) {
	var zero db.AgentTaskQueue
	if !w.SourceTaskID.Valid || !w.FilterTaskID.Valid || w.ID != squadCompletionWakeupID(w.AgentID, w.FilterTaskID) {
		return zero, nil
	}
	source, err := q.GetAgentTaskInWorkspace(ctx, db.GetAgentTaskInWorkspaceParams{ID: w.SourceTaskID, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, ErrWakeupForbidden
	}
	if err != nil {
		return zero, err
	}
	if !source.IsLeaderTask || !source.SquadID.Valid || source.AgentID != w.AgentID || source.IssueID != w.IssueID {
		return zero, ErrWakeupForbidden
	}
	squad, err := q.GetSquadInWorkspace(ctx, db.GetSquadInWorkspaceParams{ID: source.SquadID, WorkspaceID: w.WorkspaceID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (squad.ArchivedAt.Valid || squad.LeaderID != source.AgentID)) {
		return zero, ErrWakeupForbidden
	}
	return source, err
}
