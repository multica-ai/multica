package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

// QueueItem is one queued message. Item is the client's item, stored as
// given.
type QueueItem struct {
	RequestID string
	Item      json.RawMessage
	// OriginPeerID and OriginClientKind name the device that queued it: a turn runs
	// later, when no request is left to say so.
	OriginPeerID     string
	OriginClientKind string
}

// QueueState is a conversation's prompt queue.
type QueueState struct {
	Revision int64
	Paused   bool
	Error    string
	Items    []QueueItem
}

// QueueChange is one queue mutation. The caller serializes mutations and
// sets the resulting revision, pause state and error.
type QueueChange struct {
	Revision int64
	Paused   bool
	Error    string
	// Append adds an item at the end; Front adds it, or moves an existing
	// item, to the front.
	Append *QueueItem
	Front  string
	Remove string
	// RemovedAt marks Remove as the user's doing, leaving a tombstone. A message
	// leaving because it was sent has its receipt instead.
	RemovedAt time.Time
}

// queueTombstoneRetention is how long a removed message stays unable to return.
const queueTombstoneRetention = 7 * 24 * time.Hour

// Queue reads a conversation's prompt queue.
func (s *Store) Queue(ctx context.Context, conversationID string) (QueueState, error) {
	q := sqlitedb.New(s.read)
	var state QueueState
	meta, err := q.GetPromptQueue(ctx, conversationID)
	switch {
	case err == nil:
		state = QueueState{Revision: meta.Revision, Paused: meta.Paused == 1, Error: meta.Error}
	case !errors.Is(err, sql.ErrNoRows):
		return state, err
	}
	rows, err := q.ListPromptQueueItems(ctx, conversationID)
	if err != nil {
		return state, err
	}
	for _, row := range rows {
		state.Items = append(state.Items, QueueItem{RequestID: row.RequestID, Item: json.RawMessage(row.Item), OriginPeerID: row.OriginPeerID, OriginClientKind: row.OriginClientKind})
	}
	return state, nil
}

func applyQueue(ctx context.Context, q *sqlitedb.Queries, conversationID string, c *QueueChange) error {
	err := q.UpsertPromptQueue(ctx, sqlitedb.UpsertPromptQueueParams{
		ConversationID: conversationID, Revision: c.Revision, Paused: boolInt(c.Paused), Error: c.Error,
	})
	if err != nil {
		return err
	}
	if c.Remove != "" {
		if err := q.DeletePromptQueueItem(ctx, sqlitedb.DeletePromptQueueItemParams{ConversationID: conversationID, RequestID: c.Remove}); err != nil {
			return err
		}
		if !c.RemovedAt.IsZero() {
			if err := q.InsertPromptQueueTombstone(ctx, sqlitedb.InsertPromptQueueTombstoneParams{ConversationID: conversationID, RequestID: c.Remove, RemovedAt: formatTime(c.RemovedAt)}); err != nil {
				return err
			}
			if err := q.DeleteOldPromptQueueTombstones(ctx, sqlitedb.DeleteOldPromptQueueTombstonesParams{ConversationID: conversationID, RemovedAt: formatTime(c.RemovedAt.Add(-queueTombstoneRetention))}); err != nil {
				return err
			}
		}
	}
	bounds, err := q.PromptQueueBounds(ctx, conversationID)
	if err != nil {
		return err
	}
	if c.Append != nil {
		err := q.InsertPromptQueueItem(ctx, sqlitedb.InsertPromptQueueItemParams{
			ConversationID: conversationID, RequestID: c.Append.RequestID, Position: bounds.LastPosition + 1, Item: string(c.Append.Item), OriginPeerID: c.Append.OriginPeerID, OriginClientKind: c.Append.OriginClientKind,
		})
		if err != nil {
			return err
		}
	}
	if c.Front != "" {
		return q.SetPromptQueueItemPosition(ctx, sqlitedb.SetPromptQueueItemPositionParams{
			Position: bounds.FirstPosition - 1, ConversationID: conversationID, RequestID: c.Front,
		})
	}
	return nil
}

// QueueItemRemoved reports whether the user removed this message from the queue.
func (s *Store) QueueItemRemoved(ctx context.Context, conversationID, requestID string) (bool, error) {
	count, err := sqlitedb.New(s.read).CountPromptQueueTombstone(ctx, sqlitedb.CountPromptQueueTombstoneParams{ConversationID: conversationID, RequestID: requestID})
	return count > 0, err
}

// QueuedConversations lists conversations with queued messages.
func (s *Store) QueuedConversations(ctx context.Context) ([]string, error) {
	return sqlitedb.New(s.read).ListQueuedConversations(ctx)
}
