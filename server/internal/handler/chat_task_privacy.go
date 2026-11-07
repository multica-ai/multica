package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A chat session is creator-private, and so is everything its tasks produce:
// the transcript, the reply in result, and the failure text in error. Task
// lists stay workspace-wide because presence and workload views count every
// run, so they keep a foreign chat task's row and drop only its content.

// redactForeignChatTaskContent clears the content of every chat task in resp
// that viewerID did not start. tasks[i] must be the row behind resp[i]. A chat
// task whose session can no longer be resolved is treated as foreign.
func (h *Handler) redactForeignChatTaskContent(ctx context.Context, viewerID string, tasks []db.AgentTaskQueue, resp []AgentTaskResponse) error {
	seen := map[pgtype.UUID]bool{}
	var sessionIDs []pgtype.UUID
	for _, t := range tasks {
		if t.ChatSessionID.Valid && !seen[t.ChatSessionID] {
			seen[t.ChatSessionID] = true
			sessionIDs = append(sessionIDs, t.ChatSessionID)
		}
	}
	if len(sessionIDs) == 0 {
		return nil
	}
	rows, err := h.Queries.ListChatSessionCreators(ctx, sessionIDs)
	if err != nil {
		return err
	}
	owners := make(map[pgtype.UUID]string, len(rows))
	for _, row := range rows {
		owners[row.ID] = uuidToString(row.CreatorID)
	}
	for i, t := range tasks {
		if !t.ChatSessionID.Valid {
			continue
		}
		if owner, ok := owners[t.ChatSessionID]; ok && owner != "" && owner == viewerID {
			continue
		}
		resp[i].Result = nil
		resp[i].Error = nil
	}
	return nil
}
