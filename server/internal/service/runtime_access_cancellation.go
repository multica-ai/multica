package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// PrepareRuntimeAccessCancellation makes chat settlement durable in the same
// transaction as access revocation. The caller already holds the affected
// session and task locks. No events or nested transactions may run here.
//
// Every chat gets the existing finalize marker, so a crash after commit cannot
// strand its input. Started, empty turns wait for cancel-ack (or the sweeper's
// grace period); all others can use FinalizeDeferredCancelledChat immediately
// after commit. That single idempotent finalizer owns transcript messages,
// attachment detachment and persisted draft restores for disconnected clients.
func PrepareRuntimeAccessCancellation(ctx context.Context, qtx *db.Queries, tasks []db.AgentTaskQueue) ([]pgtype.UUID, error) {
	if err := SettleDeliveredDelegatedFailureRecoveries(ctx, qtx, tasks...); err != nil {
		return nil, err
	}
	var ready []pgtype.UUID
	for _, task := range tasks {
		if !task.ChatSessionID.Valid {
			continue
		}
		if err := qtx.AdvanceCancelledChatSessionPointer(ctx, task.ID); err != nil {
			return nil, fmt.Errorf("advance cancelled chat pointer: %w", err)
		}
		if _, err := qtx.MarkChatFinalizeDeferred(ctx, task.ID); err != nil {
			return nil, fmt.Errorf("mark revoked chat for settlement: %w", err)
		}
		if task.StartedAt.Valid {
			messages, err := qtx.ListTaskMessages(ctx, task.ID)
			if err != nil {
				return nil, fmt.Errorf("read revoked chat output: %w", err)
			}
			if len(messages) == 0 {
				channelIngested, err := qtx.TaskHasChannelIngestedMessages(ctx, chatInputOwnerID(task))
				if err != nil {
					return nil, fmt.Errorf("read revoked chat provenance: %w", err)
				}
				if !channelIngested {
					continue
				}
			}
		}
		ready = append(ready, task.ID)
	}
	return ready, nil
}
