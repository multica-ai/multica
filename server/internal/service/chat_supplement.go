package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// RequeueChatTaskSupplements runs under the original turn's row lock, in the
// same transaction as completion or a failed delivery. Moving the existing
// message preserves its history while giving it a new immutable input owner.
// onlyMessage restricts a provider rejection to the message it rejected.
func RequeueChatTaskSupplements(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, onlyMessage pgtype.UUID) ([]db.AgentTaskQueue, error) {
	return requeueChatTaskSupplements(ctx, q, task, onlyMessage, task.Status == "cancelled" && !onlyMessage.Valid)
}

func requeueChatTaskSupplements(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, onlyMessage pgtype.UUID, waitForDelivery bool) ([]db.AgentTaskQueue, error) {
	rows, err := q.LockUndeliveredChatTaskSupplements(ctx, task.ID)
	if err != nil {
		return nil, fmt.Errorf("lock undelivered chat supplements: %w", err)
	}
	if waitForDelivery {
		for _, row := range rows {
			if row.Status == "delivering" {
				// The daemon joins injection and acknowledgement before cancel-ack.
				// Keep later guidance here too, so it cannot overtake this message.
				_, err := q.MarkChatFinalizeDeferred(ctx, task.ID)
				return nil, err
			}
		}
	}
	var queued []db.AgentTaskQueue
	for _, row := range rows {
		if onlyMessage.Valid && row.MessageID != onlyMessage {
			continue
		}
		attr := attribution.DirectHumanRun(row.AuthorID, attribution.EvidenceChat, row.ChatSessionID)
		source, _, evidenceKind, evidenceRef := attributionCreateParams(attr)
		next, err := q.CreateChatTask(ctx, db.CreateChatTaskParams{
			ID: dbid.NewV7(), AgentID: task.AgentID, RuntimeID: task.RuntimeID,
			Priority: 3, ChatSessionID: task.ChatSessionID, InitiatorUserID: row.AuthorID,
			OriginatorUserID: row.AuthorID, AccountableUserID: row.AuthorID,
			OriginatorSource: source, TriggerEvidenceKind: evidenceKind,
			TriggerEvidenceRefID: evidenceRef,
			ForceFreshSession:    pgtype.Bool{Bool: false, Valid: true},
			RuntimeMcpOverlay:    task.RuntimeMcpOverlay, RuntimeConnectedApps: task.RuntimeConnectedApps,
		})
		if err != nil {
			return nil, fmt.Errorf("requeue chat supplement: %w", err)
		}
		next, err = q.SetChatTaskInputOwnerSelf(ctx, next.ID)
		if err != nil {
			return nil, err
		}
		if err := q.RequeueChatTaskSupplement(ctx, db.RequeueChatTaskSupplementParams{MessageID: row.MessageID, NextTaskID: next.ID}); err != nil {
			return nil, err
		}
		queued = append(queued, next)
	}
	return queued, nil
}

func (s *TaskService) publishRequeuedChatTasks(ctx context.Context, tasks []db.AgentTaskQueue) {
	for _, task := range tasks {
		s.BroadcastTaskQueued(ctx, task)
		s.NotifyTaskEnqueued(ctx, task)
	}
}
