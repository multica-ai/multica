package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChannelProvenanceQueries is the single query the reply-origin check needs.
// *db.Queries satisfies it.
type ChannelProvenanceQueries interface {
	TaskHasChannelIngestedMessages(ctx context.Context, taskID pgtype.UUID) (bool, error)
}

// TaskInputIsChannelIngested reports whether a completed chat task took its
// input from the channel, so its reply (or failure notice) belongs on the
// external platform. Direct (web/mobile) tasks can reuse a channel-bound
// session, but their replies stay in Multica (MUL-4988).
//
// chat_input_task_id alone cannot discriminate: sealed channel tasks own an
// input batch exactly like direct tasks do. The verdict is the immutable
// channel_ingested stamp on the owned batch, keyed by the batch OWNER id so an
// auto-retry clone (which inherits chat_input_task_id while its messages stay
// tagged with the parent) reaches the same verdict as its parent. A NULL owner
// is a pre-sealing channel task — direct tasks have owned their batch since
// MUL-4351 — so it keeps the deliver-by-default behavior #5645 shipped with.
func TaskInputIsChannelIngested(ctx context.Context, q ChannelProvenanceQueries, task db.AgentTaskQueue) (bool, error) {
	if !task.ChatInputTaskID.Valid {
		return true, nil
	}
	return q.TaskHasChannelIngestedMessages(ctx, task.ChatInputTaskID)
}
