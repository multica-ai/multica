package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ErrChatRetryStale means the failed turn is no longer retryable: it did not
// fail, or a later turn (including an earlier retry of it) already exists.
var ErrChatRetryStale = errors.New("chat retry: turn is no longer the latest failed turn")

// RetryFailedChatTask is a human retry of a terminally failed chat turn, e.g.
// from a channel failure card. It clones the failed attempt exactly like the
// automatic retry (same input batch and channel delivery), without the
// failure-reason or attempt-budget gates, and only while the failed turn is
// still the session's latest. The session lock serializes duplicate clicks.
func (s *TaskService) RetryFailedChatTask(ctx context.Context, parentID, actorUserID pgtype.UUID) (db.AgentTaskQueue, error) {
	if s.TxStarter == nil {
		return db.AgentTaskQueue{}, errors.New("chat retry: transaction starter is required")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := s.Queries.WithTx(tx)
	if _, err := qtx.LockChatSessionForTask(ctx, parentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, ErrChatRetryStale
		}
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: lock session: %w", err)
	}
	parent, err := qtx.GetAgentTask(ctx, parentID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: load task: %w", err)
	}
	if parent.Status != "failed" || !parent.ChatSessionID.Valid {
		return db.AgentTaskQueue{}, ErrChatRetryStale
	}
	newer, err := qtx.HasNewerChatTurnForSession(ctx, db.HasNewerChatTurnForSessionParams{
		ChatSessionID: parent.ChatSessionID, TaskID: parent.ID, CreatedAt: parent.CreatedAt,
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: check newer turn: %w", err)
	}
	if newer {
		return db.AgentTaskQueue{}, ErrChatRetryStale
	}
	var overlay runtimeMCPOverlayData
	if agent, err := qtx.GetAgent(ctx, parent.AgentID); err == nil {
		overlay = s.buildRuntimeMCPOverlay(ctx, parent.OriginatorUserID, agent)
	}
	child, err := qtx.CreateRetryTask(ctx, db.CreateRetryTaskParams{
		NewTaskID:            dbid.NewV7(),
		ID:                   parent.ID,
		RuntimeMcpOverlay:    overlay.Overlay,
		RuntimeConnectedApps: overlay.ConnectedApps,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentTaskQueue{}, ErrChatRetryStale
		}
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: create task: %w", err)
	}
	if err := qtx.CopyChannelTaskDelivery(ctx, db.CopyChannelTaskDeliveryParams{
		ChildTaskID: child.ID, ParentTaskID: parent.ID,
	}); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: copy channel delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("chat retry: commit: %w", err)
	}
	slog.Info("chat task retried by member",
		"parent_task_id", util.UUIDToString(parent.ID),
		"child_task_id", util.UUIDToString(child.ID),
		"actor_user_id", util.UUIDToString(actorUserID),
	)
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, child)
	s.NotifyTaskEnqueued(ctx, child)
	return child, nil
}
