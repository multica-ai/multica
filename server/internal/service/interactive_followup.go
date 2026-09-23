package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// CreateInteractiveFollowup participates in the caller's source-run lock and
// idempotency transaction. Notification must happen only after its commit.
func (s *TaskService) CreateInteractiveFollowup(ctx context.Context, q *db.Queries, issue db.Issue, source db.AgentTaskQueue, text string, actor pgtype.UUID) (db.AgentTaskQueue, error) {
	if !source.SessionID.Valid || source.SessionID.String == "" || source.SessionRolloutMissing {
		return db.AgentTaskQueue{}, errors.New("source run has no resumable provider session")
	}
	a, err := q.GetAgent(ctx, source.AgentID)
	if err != nil {
		return db.AgentTaskQueue{}, errors.New("agent unavailable")
	}
	var config struct {
		Enabled bool `json:"interactive_task_sessions"`
	}
	if json.Unmarshal(a.RuntimeConfig, &config) != nil || !config.Enabled || a.RuntimeID != source.RuntimeID {
		return db.AgentTaskQueue{}, errors.New("interactive setting or runtime changed; cannot safely resume this conversation")
	}
	local := &TaskService{Queries: q, Composio: s.Composio, FeatureFlags: s.FeatureFlags}
	task, err := local.insertMentionTaskWithCommentPlan(ctx, issue, source.AgentID, pgtype.UUID{}, nil, false, pgtype.UUID{}, false, text, actor, source.ID, OriginNamed)
	if err != nil {
		return task, err
	}
	if err := q.MarkTaskInteractiveFollowup(ctx, task.ID); err != nil {
		return task, err
	}
	return q.GetAgentTask(ctx, task.ID)
}

func (s *TaskService) PublishInteractiveFollowup(ctx context.Context, task db.AgentTaskQueue) {
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
}
