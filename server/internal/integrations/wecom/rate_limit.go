package wecom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	rateLimitPerMinute = 30
	rateLimitPerHour   = 1000
)

// RateGate serializes per-target window checks and records send attempts
// (spec §5.3.2).
type RateGate struct {
	q   *db.Queries
	tx  engine.TxStarter
	now func() time.Time
}

// NewRateGate builds a rate gate over generated queries.
func NewRateGate(q *db.Queries, tx engine.TxStarter) *RateGate {
	return &RateGate{q: q, tx: tx, now: time.Now}
}

// Reserve checks minute/hour windows under an advisory lock and, when allowed,
// inserts a send-attempt row. deferUntil is non-zero when the caller must defer
// without bumping queue attempts.
func (g *RateGate) Reserve(ctx context.Context, row db.ChannelOutboundQueue) (deferUntil time.Time, ok bool, err error) {
	if g.q == nil || g.tx == nil {
		return time.Time{}, false, errors.New("wecom rate gate: not configured")
	}
	tx, err := g.tx.Begin(ctx)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := g.q.WithTx(tx)

	if err := qtx.LockChannelOutboundRateWindow(ctx, db.LockChannelOutboundRateWindowParams{
		InstallationID: row.InstallationID,
		TargetChatType: row.TargetChatType,
		TargetChatID:   row.TargetChatID,
	}); err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: lock: %w", err)
	}

	now := g.now()
	minuteCount, err := qtx.CountChannelOutboundAttemptsSince(ctx, db.CountChannelOutboundAttemptsSinceParams{
		InstallationID: row.InstallationID,
		TargetChatType: row.TargetChatType,
		TargetChatID:   row.TargetChatID,
		AttemptedAt:    pgtype.Timestamptz{Time: now.Add(-time.Minute), Valid: true},
	})
	if err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: minute count: %w", err)
	}
	if minuteCount >= rateLimitPerMinute {
		return now.Add(time.Minute), false, nil
	}

	hourCount, err := qtx.CountChannelOutboundAttemptsSince(ctx, db.CountChannelOutboundAttemptsSinceParams{
		InstallationID: row.InstallationID,
		TargetChatType: row.TargetChatType,
		TargetChatID:   row.TargetChatID,
		AttemptedAt:    pgtype.Timestamptz{Time: now.Add(-time.Hour), Valid: true},
	})
	if err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: hour count: %w", err)
	}
	if hourCount >= rateLimitPerHour {
		return now.Add(time.Hour), false, nil
	}

	if _, err := qtx.RecordChannelOutboundSendAttempt(ctx, db.RecordChannelOutboundSendAttemptParams{
		QueueID:        row.ID,
		InstallationID: row.InstallationID,
		WorkspaceID:    row.WorkspaceID,
		ChatSessionID:  row.ChatSessionID,
		TargetChatID:   row.TargetChatID,
		TargetChatType: row.TargetChatType,
	}); err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: record attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, false, fmt.Errorf("wecom rate gate: commit: %w", err)
	}
	return time.Time{}, true, nil
}
