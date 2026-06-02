package service

import (
	"context"

	"github.com/dwickyfp/wallts/server/internal/util"
	db "github.com/dwickyfp/wallts/server/pkg/db/generated"
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
