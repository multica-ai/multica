package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// enqueueChatWakeupTaskTx starts one turn in a wakeup's conversation, inside
// the caller's transaction. The turn's input is a hidden wakeup row the
// daemon reads like any other input batch.
//
// A conversation still routed to an IM channel gets the same delivery
// snapshot as a channel turn, and the wakeup row is marked channel_ingested:
// outbound adapters deliver only channel-originated turns, an empty reply is
// dropped silently, and a cancelled turn settles as "Stopped." instead of
// restoring the hidden text into a composer. A retired route (the person ran
// /new) has nowhere to deliver; that turn stays in Multica.
func (s *TaskService) enqueueChatWakeupTaskTx(
	ctx context.Context,
	qtx *db.Queries,
	sessionID, wakeupID, initiatorUserID pgtype.UUID,
	content string,
	prepared PreparedChatTaskEnqueue,
) (db.AgentTaskQueue, error) {
	session, err := qtx.LockChatSessionForEnqueue(ctx, sessionID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("lock chat session: %w", err)
	}
	if session.Status != "active" {
		return db.AgentTaskQueue{}, ErrChatSessionArchived
	}
	agent, err := qtx.GetAgentForClaimUpdate(ctx, session.AgentID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("reload chat agent: %w", err)
	}
	if agent.ArchivedAt.Valid {
		return db.AgentTaskQueue{}, ErrChatTaskAgentArchived
	}
	if !agent.RuntimeID.Valid {
		return db.AgentTaskQueue{}, ErrChatTaskAgentNoRuntime
	}

	binding, bindingErr := qtx.LockChannelChatSessionBindingForContext(ctx, session.ID)
	if bindingErr != nil && !errors.Is(bindingErr, pgx.ErrNoRows) {
		return db.AgentTaskQueue{}, fmt.Errorf("lock channel chat binding: %w", bindingErr)
	}
	deliver := bindingErr == nil && !binding.RetiredAt.Valid
	var contextRevision int64
	forceFresh := false
	if bindingErr == nil {
		contextRevision = binding.ContextRevision
		generation, err := qtx.LockChannelChatContextGenerationByRevision(ctx, db.LockChannelChatContextGenerationByRevisionParams{
			ChatSessionID: session.ID, Revision: contextRevision,
		})
		if err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("lock channel context generation: %w", err)
		}
		forceFresh = generation.PendingFresh
	}

	task, err := qtx.CreateChatTask(ctx, db.CreateChatTaskParams{
		ID:                   dbid.NewV7(),
		AgentID:              session.AgentID,
		RuntimeID:            agent.RuntimeID,
		Priority:             2,
		ChatSessionID:        session.ID,
		InitiatorUserID:      initiatorUserID,
		OriginatorUserID:     initiatorUserID,
		AccountableUserID:    prepared.accountableUser,
		ForceFreshSession:    pgtype.Bool{Bool: forceFresh, Valid: true},
		RuntimeMcpOverlay:    prepared.runtimeOverlay.Overlay,
		RuntimeConnectedApps: prepared.runtimeOverlay.ConnectedApps,
		OriginatorSource:     prepared.attrSource,
		TriggerEvidenceKind:  prepared.attrEvidenceKind,
		TriggerEvidenceRefID: session.ID,
		ChannelContextRevision: pgtype.Int8{
			Int64: contextRevision, Valid: contextRevision > 0,
		},
	})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create chat wakeup task: %w", err)
	}
	if deliver {
		if _, err := qtx.CreateChannelTaskDeliveryFromSession(ctx, db.CreateChannelTaskDeliveryFromSessionParams{
			TaskID: task.ID, ChatSessionID: session.ID, ContextRevision: contextRevision,
		}); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("snapshot channel task delivery: %w", err)
		}
	}
	task, err = qtx.SetChatTaskInputOwnerSelf(ctx, task.ID)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("set chat wakeup input owner: %w", err)
	}
	if _, err := qtx.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ID:                     dbid.NewV7(),
		ChatSessionID:          session.ID,
		Role:                   "user",
		Content:                content,
		TaskID:                 task.ID,
		MessageKind:            pgtype.Text{String: protocol.ChatMessageKindWakeup, Valid: true},
		ChannelIngested:        pgtype.Bool{Bool: deliver, Valid: true},
		ChannelContextRevision: pgtype.Int8{Int64: contextRevision, Valid: contextRevision > 0},
		WakeupID:               wakeupID,
	}); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create chat wakeup input: %w", err)
	}
	if forceFresh {
		if err := qtx.ClearChannelChatContextPendingFresh(ctx, db.ClearChannelChatContextPendingFreshParams{
			ChatSessionID: session.ID, Revision: contextRevision,
		}); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("clear channel context pending fresh: %w", err)
		}
		if err := qtx.ClearChannelChatSessionPendingFreshForRevision(ctx, db.ClearChannelChatSessionPendingFreshForRevisionParams{
			ChatSessionID: session.ID, Revision: contextRevision,
		}); err != nil {
			return db.AgentTaskQueue{}, fmt.Errorf("clear channel pending fresh: %w", err)
		}
	}
	if err := qtx.TouchChatSession(ctx, session.ID); err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("touch chat session: %w", err)
	}
	return task, nil
}

// finalizeChatWakeupEnqueue performs the post-commit effects of a wakeup turn.
// It is not a human delegation, so it skips FinalizeChatTaskEnqueue's ledger.
func (s *TaskService) finalizeChatWakeupEnqueue(ctx context.Context, task db.AgentTaskQueue) {
	s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, task)
	s.NotifyTaskEnqueued(ctx, task)
}

// chatWakeupTurnInput reports the hidden wakeup row behind a chat turn, if the
// turn was started by a wakeup.
func chatWakeupTurnInput(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue) (db.ChatMessage, bool, error) {
	if !task.ChatSessionID.Valid {
		return db.ChatMessage{}, false, nil
	}
	row, err := qtx.GetChatWakeupInputForTask(ctx, chatInputOwnerID(task))
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChatMessage{}, false, nil
	}
	if err != nil {
		return db.ChatMessage{}, false, fmt.Errorf("load chat wakeup input: %w", err)
	}
	return row, true, nil
}

// chatWakeupTurnIsSilent reports whether a wakeup turn ends without a reply:
// a check that checked in, or one that wrote nothing. Its hidden row is
// removed, so the conversation shows no trace of a quiet check.
func chatWakeupTurnIsSilent(ctx context.Context, qtx *db.Queries, task db.AgentTaskQueue, empty bool) (bool, error) {
	row, ok, err := chatWakeupTurnInput(ctx, qtx, task)
	if err != nil || !ok {
		return false, err
	}
	silent := empty
	if !silent && row.WakeupID.Valid {
		w, err := qtx.GetChatWakeup(ctx, row.WakeupID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("load chat wakeup: %w", err)
		}
		silent = err == nil && w.LastCheckinTaskID.Valid &&
			(w.LastCheckinTaskID == task.ID || w.LastCheckinTaskID == chatInputOwnerID(task))
	}
	if !silent {
		return false, nil
	}
	if err := qtx.DeleteChatWakeupInput(ctx, row.ID); err != nil {
		return false, fmt.Errorf("delete silent chat wakeup input: %w", err)
	}
	return true, nil
}
