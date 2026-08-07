package outbox

// rategate.go — the shared per-target rate gate. The windows and limits are the
// channel's (each platform publishes its own quotas); the check-then-record
// sequence, its serialization, and the "a deferral is not a failure" contract
// are the same everywhere, so they live here.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Window is one quota the gate enforces: at most Limit attempts to a single
// target within Duration.
type Window struct {
	// Name identifies the window in error text and logs ("minute", "hour").
	Name     string
	Duration time.Duration
	Limit    int64
}

// TxStarter begins the transaction the gate's lock, counts, and insert share.
// *pgxpool.Pool satisfies it.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// RateGateQueries is the transaction-bound query surface the gate uses.
// *db.Queries satisfies it.
type RateGateQueries interface {
	LockChannelOutboundRateWindow(ctx context.Context, arg db.LockChannelOutboundRateWindowParams) error
	CountChannelOutboundAttemptsSince(ctx context.Context, arg db.CountChannelOutboundAttemptsSinceParams) (int64, error)
	RecordChannelOutboundSendAttempt(ctx context.Context, arg db.RecordChannelOutboundSendAttemptParams) (db.ChannelOutboundSendAttempt, error)
}

// BindTx binds the generated queries to a transaction. Boot passes
// `func(tx pgx.Tx) outbox.RateGateQueries { return queries.WithTx(tx) }` — a
// function rather than a store interface because Queries.WithTx returns the
// concrete *db.Queries, which no interface-returning method set can express.
type BindTx func(tx pgx.Tx) RateGateQueries

// WindowRateGate admits sends against per-target sliding windows recorded in
// channel_outbound_send_attempt.
//
// Why the ledger and not an in-process token bucket: the quota is enforced by
// the platform, per target chat. A bucket only knows what this replica sent, so
// after a lease flip the new holder starts from zero and walks straight back
// into the limit — exactly when a backlog is being drained and the limit
// matters most.
type WindowRateGate struct {
	bind    BindTx
	tx      TxStarter
	windows []Window
	now     func() time.Time
}

var _ RateGate = (*WindowRateGate)(nil)

// NewWindowRateGate builds a gate over the given windows. Order does not affect
// correctness — every window is checked — but listing the shortest first makes
// the common rejection the cheapest.
func NewWindowRateGate(bind BindTx, tx TxStarter, windows ...Window) (*WindowRateGate, error) {
	if bind == nil || tx == nil {
		return nil, errors.New("outbox: rate gate requires a tx binder and a tx starter")
	}
	if len(windows) == 0 {
		return nil, errors.New("outbox: rate gate requires at least one window")
	}
	for _, w := range windows {
		if w.Duration <= 0 || w.Limit <= 0 {
			return nil, fmt.Errorf("outbox: rate gate window %q needs a positive duration and limit", w.Name)
		}
	}
	return &WindowRateGate{bind: bind, tx: tx, windows: windows, now: time.Now}, nil
}

// Reserve checks every window under an advisory lock and, when all pass, records
// the attempt.
//
// The lock, the counts, and the insert are one transaction on purpose. Without
// it two workers draining the same target both count under the limit and both
// send, so the gate leaks as many sends as there are concurrent drainers — and
// the drain after a reconnect is exactly when that happens.
//
// ok=false returns the time the caller should defer to. The consumer defers
// without spending an attempt: a target that is merely over quota has not
// failed, and charging it a retry would eventually dead-letter a message that
// was never tried.
func (g *WindowRateGate) Reserve(ctx context.Context, row db.ChannelOutboundQueue) (time.Time, bool, error) {
	tx, err := g.tx.Begin(ctx)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("outbox rate gate: begin tx: %w", err)
	}
	// Rollback on every path that does not reach Commit. It is a no-op after a
	// successful commit, and it is what releases the transaction-scoped
	// advisory lock when a window rejects.
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := g.bind(tx)

	if err := qtx.LockChannelOutboundRateWindow(ctx, db.LockChannelOutboundRateWindowParams{
		InstallationID: row.InstallationID,
		TargetChatType: row.TargetChatType,
		TargetChatID:   row.TargetChatID,
	}); err != nil {
		return time.Time{}, false, fmt.Errorf("outbox rate gate: lock: %w", err)
	}

	now := g.now()
	for _, w := range g.windows {
		count, err := qtx.CountChannelOutboundAttemptsSince(ctx, db.CountChannelOutboundAttemptsSinceParams{
			InstallationID: row.InstallationID,
			TargetChatType: row.TargetChatType,
			TargetChatID:   row.TargetChatID,
			AttemptedAt:    pgtype.Timestamptz{Time: now.Add(-w.Duration), Valid: true},
		})
		if err != nil {
			return time.Time{}, false, fmt.Errorf("outbox rate gate: count %s window: %w", w.Name, err)
		}
		if count >= w.Limit {
			// Defer by the full window rather than computing when the oldest
			// attempt ages out: one extra query per rejection buys nothing, and
			// the queue keeps its order either way.
			return now.Add(w.Duration), false, nil
		}
	}

	if _, err := qtx.RecordChannelOutboundSendAttempt(ctx, db.RecordChannelOutboundSendAttemptParams{
		QueueID:        row.ID,
		InstallationID: row.InstallationID,
		WorkspaceID:    row.WorkspaceID,
		ChatSessionID:  row.ChatSessionID,
		TargetChatID:   row.TargetChatID,
		TargetChatType: row.TargetChatType,
	}); err != nil {
		return time.Time{}, false, fmt.Errorf("outbox rate gate: record attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return time.Time{}, false, fmt.Errorf("outbox rate gate: commit: %w", err)
	}
	return time.Time{}, true, nil
}
