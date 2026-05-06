package channel

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MessageService owns channel_message reads and writes. Like ChannelService
// it has no platform integrations — handlers do mention parsing, task queue
// fan-out, and inbox writes around the service's plain data calls.
type MessageService struct {
	Queries *db.Queries
}

// NewMessageService constructs a MessageService.
func NewMessageService(q *db.Queries) *MessageService {
	return &MessageService{Queries: q}
}

// DefaultPageLimit and MaxPageLimit are the page-size guardrails for List.
// MaxPageLimit guards memory and frontend rendering; the wire protocol clamps
// at this value, callers don't have to.
const (
	DefaultPageLimit int32 = 50
	MaxPageLimit     int32 = 200
)

// Create inserts a new message into a channel. The author's membership is
// NOT verified here — handlers do that as part of authorization. This keeps
// the service usable from background jobs (e.g. retention sweep posting an
// admin notice) without an extra query.
func (s *MessageService) Create(ctx context.Context, p CreateMessageParams) (db.ChannelMessage, error) {
	if err := validateActorType(p.Author.Type); err != nil {
		return db.ChannelMessage{}, err
	}
	if p.Content == "" {
		return db.ChannelMessage{}, fmt.Errorf("%w: content must not be empty", ErrInvalid)
	}

	args := db.CreateChannelMessageParams{
		ChannelID:  p.ChannelID,
		AuthorType: p.Author.Type,
		AuthorID:   p.Author.ID,
		Content:    p.Content,
	}
	if p.ParentMessageID != nil {
		args.ParentMessageID = *p.ParentMessageID
	}
	if len(p.Metadata) > 0 {
		args.Metadata = p.Metadata
	}
	return s.Queries.CreateChannelMessage(ctx, args)
}

// Get returns a single message by id. Returns ErrNotFound if absent.
func (s *MessageService) Get(ctx context.Context, id pgtype.UUID) (db.ChannelMessage, error) {
	m, err := s.Queries.GetChannelMessage(ctx, id)
	return m, translateNotFound(err)
}

// List returns messages in a channel, newest first, with cursor-based
// pagination. The default view excludes thread replies; pass
// IncludeThreaded=true for the full stream (used by search and sidecars).
//
// The Limit field is clamped to [1, MaxPageLimit]; 0 falls back to the
// default. BeforeCreatedAt nil returns the newest page.
func (s *MessageService) List(ctx context.Context, p ListMessagesParams) ([]db.ChannelMessage, error) {
	limit := p.Limit
	if limit <= 0 {
		limit = DefaultPageLimit
	}
	if limit > MaxPageLimit {
		limit = MaxPageLimit
	}
	before := pgtype.Timestamptz{}
	if p.BeforeCreatedAt != nil {
		before = *p.BeforeCreatedAt
	}

	if p.IncludeThreaded {
		return s.Queries.ListChannelMessagesIncludingThreads(ctx, db.ListChannelMessagesIncludingThreadsParams{
			ChannelID:       p.ChannelID,
			BeforeCreatedAt: before,
			Limit:           limit,
		})
	}
	return s.Queries.ListChannelMessages(ctx, db.ListChannelMessagesParams{
		ChannelID:       p.ChannelID,
		BeforeCreatedAt: before,
		Limit:           limit,
	})
}

// ListThread returns the replies under a parent message in chronological
// order. Soft-deleted replies are excluded.
func (s *MessageService) ListThread(ctx context.Context, parentID pgtype.UUID) ([]db.ChannelMessage, error) {
	return s.Queries.ListThreadReplies(ctx, parentID)
}

// Count returns the number of non-deleted messages in a channel.
func (s *MessageService) Count(ctx context.Context, channelID pgtype.UUID) (int64, error) {
	return s.Queries.CountChannelMessages(ctx, channelID)
}

// SoftDeleteOldMessages drains messages older than `before` from one
// channel in batches of `batchSize`. Returns the number of messages
// soft-deleted in this batch; zero means the channel is drained. Phase 2
// uses this from the retention cron.
func (s *MessageService) SoftDeleteOldMessages(ctx context.Context, channelID pgtype.UUID, before time.Time, batchSize int32) (int64, error) {
	if batchSize <= 0 {
		batchSize = 1000
	}
	return s.Queries.SoftDeleteOldChannelMessages(ctx, db.SoftDeleteOldChannelMessagesParams{
		ChannelID: channelID,
		CreatedAt: pgtype.Timestamptz{Time: before, Valid: true},
		Limit:     batchSize,
	})
}
