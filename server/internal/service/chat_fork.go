package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const NativeChatForkTaskContext = `{"native_chat_fork":true}`

func isNativeChatForkTask(task db.AgentTaskQueue) bool {
	if !task.ChatSessionID.Valid || len(task.Context) == 0 {
		return false
	}
	var marker struct {
		NativeChatFork bool `json:"native_chat_fork"`
	}
	return json.Unmarshal(task.Context, &marker) == nil && marker.NativeChatFork
}

// settleNativeChatFork runs under the destination chat lock and in the same
// transaction as its task's terminal transition.
func settleNativeChatFork(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, childSessionID string) error {
	child, err := q.GetChatSession(ctx, task.ChatSessionID)
	if err != nil {
		return fmt.Errorf("load pending chat fork: %w", err)
	}
	if child.ForkInitializationTaskID != task.ID || !child.ForkedFromChatID.Valid {
		return errors.New("terminal task does not own the pending chat fork")
	}
	if task.Status == "completed" {
		if err := uuid.Validate(childSessionID); err != nil {
			return errors.New("native chat fork did not return a valid child session")
		}
		if _, err := q.CompletePendingChatFork(ctx, db.CompletePendingChatForkParams{
			DestinationChatID: child.ID, TaskID: task.ID,
			SessionID: pgtype.Text{String: childSessionID, Valid: true},
		}); err != nil {
			return fmt.Errorf("complete pending chat fork: %w", err)
		}
	} else {
		if _, err := q.DeletePendingChatFork(ctx, db.DeletePendingChatForkParams{
			DestinationChatID: child.ID, TaskID: task.ID,
		}); err != nil {
			return fmt.Errorf("discard pending chat fork: %w", err)
		}
	}
	_, err = q.ReleaseChatForkSnapshot(ctx, db.ReleaseChatForkSnapshotParams{
		SourceChatID: child.ForkedFromChatID, TaskID: task.ID,
	})
	return err
}
