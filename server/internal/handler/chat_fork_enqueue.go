package handler

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/attribution"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

var errChatForkUnavailable = errors.New("chat fork requires an idle chat on its original compatible Codex runtime")

// queueChatFork is called only after verifying that the initiating member
// owns the source chat and may invoke its agent.
func (h *Handler) queueChatFork(ctx context.Context, source db.ChatSession, creatorID pgtype.UUID) (db.ChatSession, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	defer tx.Rollback(ctx)
	q := h.Queries.WithTx(tx)
	current, err := q.LockChatSessionForEnqueue(ctx, source.ID)
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	if current.CreatorID != creatorID || current.WorkspaceID != source.WorkspaceID || current.AgentID != source.AgentID {
		return db.ChatSession{}, db.AgentTaskQueue{}, errChatForkUnavailable
	}
	if _, err := q.LockAgentForTaskEnqueue(ctx, current.AgentID); err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	agent, err := q.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: current.AgentID, WorkspaceID: current.WorkspaceID})
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	if agent.ArchivedAt.Valid || !current.RuntimeID.Valid || agent.RuntimeID != current.RuntimeID {
		return db.ChatSession{}, db.AgentTaskQueue{}, errChatForkUnavailable
	}
	runtime, err := q.GetAgentRuntime(ctx, current.RuntimeID)
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	if runtime.Provider != "codex" || runtime.RuntimeMode != "local" ||
		!runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityCodexChatForkV1) {
		return db.ChatSession{}, db.AgentTaskQueue{}, errChatForkUnavailable
	}

	taskID := dbid.NewV7()
	if _, err := q.ReserveChatForkSnapshot(ctx, db.ReserveChatForkSnapshotParams{
		SourceChatID: current.ID, WorkspaceID: current.WorkspaceID, CreatorID: creatorID, TaskID: taskID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return db.ChatSession{}, db.AgentTaskQueue{}, errChatForkUnavailable
	} else if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	child, err := q.CreatePendingChatFork(ctx, db.CreatePendingChatForkParams{
		SourceChatID: current.ID, DestinationChatID: dbid.NewV7(), TaskID: taskID,
	})
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	task, err := q.CreateChatTask(ctx, db.CreateChatTaskParams{
		ID: taskID, AgentID: current.AgentID, RuntimeID: current.RuntimeID,
		Priority: 2, ChatSessionID: child.ID, InitiatorUserID: creatorID,
		OriginatorUserID: creatorID, AccountableUserID: creatorID,
		OriginatorSource:     pgtype.Text{String: string(attribution.SourceDirectHuman), Valid: true},
		TriggerEvidenceKind:  pgtype.Text{String: string(attribution.EvidenceChat), Valid: true},
		TriggerEvidenceRefID: current.ID,
	})
	if err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	if task.RuntimeID != current.RuntimeID {
		return db.ChatSession{}, db.AgentTaskQueue{}, errChatForkUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ChatSession{}, db.AgentTaskQueue{}, err
	}
	return child, task, nil
}
