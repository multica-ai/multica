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

// RetentionSweepStats reports per-run aggregate counts. The cron loop logs
// these so operators can see "did anything happen?" at a glance.
type RetentionSweepStats struct {
	ChannelsScanned int
	MessagesDeleted int64
}

// RunRetentionSweep performs the daily retention pass: iterate every
// non-archived channel whose effective retention is finite, soft-delete
// messages older than the cutoff in batches of `batchSize`. Idempotent —
// already-deleted rows are filtered by the underlying query's WHERE.
//
// `now` is injectable so tests can pin a deterministic cutoff. Production
// callers pass time.Now().UTC().
//
// Failure mode: if a single channel's batched delete errors, we log and
// continue — one bad workspace shouldn't stall the rest of the sweep.
// The error is captured in stats so callers can decide whether to alert.
func (s *MessageService) RunRetentionSweep(ctx context.Context, now time.Time, batchSize int32) (RetentionSweepStats, error) {
	candidates, err := s.Queries.ListChannelsWithRetention(ctx)
	if err != nil {
		return RetentionSweepStats{}, fmt.Errorf("retention sweep: list candidates: %w", err)
	}
	if batchSize <= 0 {
		batchSize = 1000
	}

	var stats RetentionSweepStats
	for _, c := range candidates {
		if c.EffectiveDays <= 0 {
			// Defensive: the SQL filter excludes <=0, but a future schema
			// change to allow 0 ("immediate") shouldn't accidentally wipe
			// a workspace mid-deploy. Skip rather than interpret.
			continue
		}
		stats.ChannelsScanned++
		cutoff := now.Add(-time.Duration(c.EffectiveDays) * 24 * time.Hour)

		// Drain the channel in batches. We cap the inner loop at 100 rounds
		// (= 100 × batchSize messages) per channel per run so a single
		// pathological channel can't monopolize one sweep — rare, but
		// retention can be flipped from "forever" to "30 days" on a
		// chatty long-lived channel and produce a huge candidate set.
		for round := 0; round < 100; round++ {
			n, err := s.SoftDeleteOldMessages(ctx, c.ChannelID, cutoff, batchSize)
			if err != nil {
				return stats, fmt.Errorf("retention sweep: channel %s: %w", uuidString(c.ChannelID), err)
			}
			stats.MessagesDeleted += n
			if n < int64(batchSize) {
				break
			}
		}
	}
	return stats, nil
}
